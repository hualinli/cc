package middleware

import (
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMiddlewareTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&models.Node{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		models.DB = oldDB
	}
}

func performRequestWithSession(t *testing.T, method, path string, role any, userID any, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))

	r.GET(path, func(c *gin.Context) {
		session := sessions.Default(c)
		if role != nil {
			session.Set("role", role)
		}
		if userID != nil {
			session.Set("user_id", userID)
		}
		if err := session.Save(); err != nil {
			t.Fatalf("failed to save session: %v", err)
		}
		c.Next()
	}, handler)

	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_PageRedirectsWhenNotAuthenticated(t *testing.T) {
	w := performRequestWithSession(t, http.MethodGet, "/protected", nil, nil, func(c *gin.Context) {
		AuthMiddleware()(c)
		if c.IsAborted() {
			return
		}
		c.String(http.StatusOK, "ok")
	})

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", w.Code)
	}
	if location := w.Header().Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %s", location)
	}
}

func TestAuthMiddleware_APIUnauthorizedWhenNotAuthenticated(t *testing.T) {
	w := performRequestWithSession(t, http.MethodGet, "/api/test", nil, nil, func(c *gin.Context) {
		AuthMiddleware()(c)
		if c.IsAborted() {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "unauthorized" {
		t.Fatalf("expected unauthorized error, got %v", resp["error"])
	}
}

func TestAdminMiddleware_PageForbiddenWhenNotAdmin(t *testing.T) {
	w := performRequestWithSession(t, http.MethodGet, "/admin/test", "proctor", uint(1), func(c *gin.Context) {
		AdminMiddleware()(c)
		if c.IsAborted() {
			return
		}
		c.String(http.StatusOK, "ok")
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestAdminMiddleware_APIForbiddenWhenNotAdmin(t *testing.T) {
	w := performRequestWithSession(t, http.MethodGet, "/api/admin/test", "proctor", uint(1), func(c *gin.Context) {
		AdminMiddleware()(c)
		if c.IsAborted() {
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "forbidden" {
		t.Fatalf("expected forbidden error, got %v", resp["error"])
	}
}

func TestNodeAuthMiddleware_Success(t *testing.T) {
	cleanup := setupMiddlewareTestDB(t)
	defer cleanup()

	node := models.Node{Name: "node-1", Token: "token-1"}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/node-api/v1/test", NodeAuthMiddleware(), func(c *gin.Context) {
		nodeID, _ := c.Get("node_id")
		nodeName, _ := c.Get("node_name")
		c.JSON(http.StatusOK, gin.H{"node_id": nodeID, "node_name": nodeName})
	})

	req := httptest.NewRequest(http.MethodGet, "/node-api/v1/test", nil)
	req.Header.Set("X-Node-Token", "token-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["node_name"] != "node-1" {
		t.Fatalf("expected node_name node-1, got %v", resp["node_name"])
	}
}

func TestNodeAuthMiddleware_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/node-api/v1/test", NodeAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/node-api/v1/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "missing token" {
		t.Fatalf("expected missing token error, got %v", resp["error"])
	}
}

func TestNodeAuthMiddleware_InvalidToken(t *testing.T) {
	cleanup := setupMiddlewareTestDB(t)
	defer cleanup()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/node-api/v1/test", NodeAuthMiddleware(), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/node-api/v1/test", nil)
	req.Header.Set("X-Node-Token", "bad-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "invalid token" {
		t.Fatalf("expected invalid token error, got %v", resp["error"])
	}
}
