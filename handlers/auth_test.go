package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"cc/models"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAuthHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		models.DB = oldDB
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

func seedAuthUser(t *testing.T, username string, role models.UserRole) models.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	user := models.User{
		Username: username,
		Password: string(hash),
		Role:     role,
	}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return user
}

func setupAuthRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.POST("/login", LoginPostHandler)
	r.GET("/logout", LogoutHandler)
	return r
}

func TestLoginPostHandler_AdminRedirectsToAdminRoot(t *testing.T) {
	cleanup := setupAuthHandlerTestDB(t)
	defer cleanup()

	seedAuthUser(t, "admin-user", models.Admin)

	r := setupAuthRouter()
	form := url.Values{
		"username": {"admin-user"},
		"password": {"secret-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp["success"])
	}
	if resp["redirect"] != "/admin/" {
		t.Fatalf("expected redirect /admin/, got %v", resp["redirect"])
	}
	if w.Header().Get("Set-Cookie") == "" {
		t.Fatal("expected session cookie to be set")
	}
}

func TestLoginPostHandler_ProctorRedirectsToRoot(t *testing.T) {
	cleanup := setupAuthHandlerTestDB(t)
	defer cleanup()

	seedAuthUser(t, "proctor-user", models.Proctor)

	r := setupAuthRouter()
	form := url.Values{
		"username": {"proctor-user"},
		"password": {"secret-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["redirect"] != "/" {
		t.Fatalf("expected redirect /, got %v", resp["redirect"])
	}
}

func TestLoginPostHandler_InvalidUsernameReturnsUnauthorized(t *testing.T) {
	cleanup := setupAuthHandlerTestDB(t)
	defer cleanup()

	r := setupAuthRouter()
	form := url.Values{
		"username": {"missing-user"},
		"password": {"secret-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "用户不存在" {
		t.Fatalf("expected 用户不存在, got %v", resp["error"])
	}
}

func TestLoginPostHandler_InvalidPasswordReturnsUnauthorized(t *testing.T) {
	cleanup := setupAuthHandlerTestDB(t)
	defer cleanup()

	seedAuthUser(t, "proctor-user", models.Proctor)

	r := setupAuthRouter()
	form := url.Values{
		"username": {"proctor-user"},
		"password": {"wrong-pass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "密码错误" {
		t.Fatalf("expected 密码错误, got %v", resp["error"])
	}
}

func TestLogoutHandler_ReturnsLoginRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.GET("/set-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", uint(1))
		if err := session.Save(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "session 保存失败"})
			return
		}
		c.Status(http.StatusOK)
	})
	r.GET("/logout", LogoutHandler)

	req := httptest.NewRequest(http.MethodGet, "/set-session", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	cookie := w.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("expected session cookie to be returned by set-session")
	}

	req = httptest.NewRequest(http.MethodGet, "/logout", nil)
	req.Header.Set("Cookie", cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["redirect"] != "/login" {
		t.Fatalf("expected redirect /login, got %v", resp["redirect"])
	}
	if w.Header().Get("Set-Cookie") == "" {
		t.Fatal("expected session cookie to be returned by logout")
	}
}
