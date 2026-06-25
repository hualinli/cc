package handlers

import (
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupNodePaginationTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.Node{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	models.DB = db

	return func() {
		models.DB = oldDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}
}

func seedNodePaginationUser(t *testing.T, username string, role models.UserRole) models.User {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.DefaultCost)
	user := models.User{Username: username, Password: string(hash), Role: role}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedNodePaginationNode(t *testing.T, name string) models.Node {
	t.Helper()
	node := models.Node{
		Name:      name,
		Token:     "token-" + name,
		NodeModel: "model-1",
		Address:   "127.0.0.1:8080",
		Status:    models.NodeStatusIdle,
	}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

func performListNodesWithPagination(t *testing.T, query string, userID uint, role string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", userID)
		session.Set("role", role)
		_ = session.Save()
		c.Next()
	})
	r.GET("/nodes", ListNodes)

	req := httptest.NewRequest(http.MethodGet, "/nodes"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListNodesPagination(t *testing.T) {
	cleanup := setupNodePaginationTestDB(t)
	defer cleanup()

	admin := seedNodePaginationUser(t, "admin", models.Admin)

	// 创建 25 个节点
	for i := 1; i <= 25; i++ {
		seedNodePaginationNode(t, "node-"+string(rune('A'+i-1)))
	}

	tests := []struct {
		name              string
		query             string
		expectedCode      int
		expectedPageSize  int
		expectedPage      int
		expectedDataCount int
		expectedTotal     int64
	}{
		{
			name:              "default pagination",
			query:             "",
			expectedCode:      http.StatusOK,
			expectedPageSize:  20,
			expectedPage:      1,
			expectedDataCount: 20,
			expectedTotal:     25,
		},
		{
			name:              "page 2",
			query:             "?page=2",
			expectedCode:      http.StatusOK,
			expectedPageSize:  20,
			expectedPage:      2,
			expectedDataCount: 5,
			expectedTotal:     25,
		},
		{
			name:              "custom page size",
			query:             "?page=1&page_size=10",
			expectedCode:      http.StatusOK,
			expectedPageSize:  10,
			expectedPage:      1,
			expectedDataCount: 10,
			expectedTotal:     25,
		},
		{
			name:              "page size too large",
			query:             "?page_size=200",
			expectedCode:      http.StatusOK,
			expectedPageSize:  100,
			expectedPage:      1,
			expectedDataCount: 25,
			expectedTotal:     25,
		},
		{
			name:              "invalid page",
			query:             "?page=0",
			expectedCode:      http.StatusOK,
			expectedPageSize:  20,
			expectedPage:      1,
			expectedDataCount: 20,
			expectedTotal:     25,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := performListNodesWithPagination(t, tc.query, admin.ID, string(admin.Role))

			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d", tc.expectedCode, w.Code)
			}

			var resp struct {
				Success    bool          `json:"success"`
				Data       []nodePayload `json:"data"`
				Pagination struct {
					Page     int   `json:"page"`
					PageSize int   `json:"page_size"`
					Total    int64 `json:"total"`
				} `json:"pagination"`
			}

			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if !resp.Success {
				t.Fatalf("expected success true, got false")
			}

			if len(resp.Data) != tc.expectedDataCount {
				t.Fatalf("expected %d nodes, got %d", tc.expectedDataCount, len(resp.Data))
			}

			if resp.Pagination.Page != tc.expectedPage {
				t.Fatalf("expected page %d, got %d", tc.expectedPage, resp.Pagination.Page)
			}

			if resp.Pagination.PageSize != tc.expectedPageSize {
				t.Fatalf("expected page_size %d, got %d", tc.expectedPageSize, resp.Pagination.PageSize)
			}

			if resp.Pagination.Total != tc.expectedTotal {
				t.Fatalf("expected total %d, got %d", tc.expectedTotal, resp.Pagination.Total)
			}
		})
	}
}
