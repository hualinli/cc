package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUsersHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test db: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		_ = db.Migrator().DropTable(&models.User{})
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		models.DB = oldDB
	}
}

func performCreateUserRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/users", CreateUser)

	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func performGetUserRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/users/:id", GetUser)

	req := httptest.NewRequest(http.MethodGet, "/users/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func performListUsersRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/users", ListUsers)

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func performDeleteUserRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/users/:id", DeleteUser)

	req := httptest.NewRequest(http.MethodDelete, "/users/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func performUpdateUserRequest(t *testing.T, id string, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/users/:id", UpdateUser)

	req := httptest.NewRequest(http.MethodPut, "/users/"+id, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func performChangePasswordRequest(t *testing.T, sessionUserID any, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.Use(func(c *gin.Context) {
		if sessionUserID != nil {
			session := sessions.Default(c)
			session.Set("user_id", sessionUserID)
			if err := session.Save(); err != nil {
				t.Fatalf("failed to save test session: %v", err)
			}
		}
		c.Next()
	})
	r.PUT("/users/password", ChangePassword)

	req := httptest.NewRequest(http.MethodPut, "/users/password", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	return w
}

func TestCreateUser(t *testing.T) {
	testCases := []struct {
		name                 string
		requestBody          string
		seed                 bool
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		expectUserCreated    bool
		expectPasswordHash   bool
	}{
		{
			name:                 "create user success",
			requestBody:          `{"username":"alice","password":"pass123","role":"proctor"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			expectUserCreated:    true,
			expectPasswordHash:   true,
		},
		{
			name:                 "duplicate username",
			requestBody:          `{"username":"alice","password":"pass123","role":"proctor"}`,
			seed:                 true,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "用户名已存在",
			expectSuccess:        false,
		},
		{
			name:                 "invalid input missing username",
			requestBody:          `{"password":"pass123","role":"proctor"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "输入错误",
			expectSuccess:        false,
		},
		{
			name:                 "invalid role",
			requestBody:          `{"username":"alice","password":"pass123","role":"guest"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "角色非法",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			if tc.seed {
				seedHash, err := bcrypt.GenerateFromPassword([]byte("seed-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				seed := models.User{Username: "alice", Password: string(seedHash), Role: models.Proctor}
				if err := models.DB.Create(&seed).Error; err != nil {
					t.Fatalf("failed to seed user: %v", err)
				}
			}

			w := performCreateUserRequest(t, tc.requestBody)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}

			if tc.expectUserCreated {
				var user models.User
				if err := models.DB.Where("username = ?", "alice").First(&user).Error; err != nil {
					t.Fatalf("expected user in db, got err=%v", err)
				}
				if strings.Contains(w.Body.String(), "password") {
					t.Fatalf("response should not include password field")
				}
				if tc.expectPasswordHash {
					if user.Password == "pass123" {
						t.Fatalf("expected password to be hashed, got plain text")
					}
					if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("pass123")); err != nil {
						t.Fatalf("stored hash does not match password: %v", err)
					}
				}
			}
		})
	}
}

func TestGetUser(t *testing.T) {
	testCases := []struct {
		name                 string
		id                   string
		seed                 bool
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
	}{
		{
			name:                 "get user success",
			id:                   "1",
			seed:                 true,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"username":"alice"`,
			expectSuccess:        true,
		},
		{
			name:                 "user not found",
			id:                   "999",
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "用户不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			if tc.seed {
				seedHash, err := bcrypt.GenerateFromPassword([]byte("seed-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				seed := models.User{Username: "alice", Password: string(seedHash), Role: models.Proctor}
				if err := models.DB.Create(&seed).Error; err != nil {
					t.Fatalf("failed to seed user: %v", err)
				}
			}

			w := performGetUserRequest(t, tc.id)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}

			if tc.expectSuccess && strings.Contains(w.Body.String(), "password") {
				t.Fatalf("response should not include password field")
			}
		})
	}
}

func TestListUsers(t *testing.T) {
	testCases := []struct {
		name               string
		seedUsers          []string
		expectedCode       int
		expectedDataCount  int
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name:               "empty list",
			expectedCode:       http.StatusOK,
			expectedDataCount:  0,
			expectedContains:   []string{`"success":true`, `"data":[]`},
			expectedNotContain: []string{"password"},
		},
		{
			name:               "list with users",
			seedUsers:          []string{"alice", "bob"},
			expectedCode:       http.StatusOK,
			expectedDataCount:  2,
			expectedContains:   []string{`"username":"alice"`, `"username":"bob"`, `"success":true`},
			expectedNotContain: []string{"password"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			for _, username := range tc.seedUsers {
				hash, err := bcrypt.GenerateFromPassword([]byte(username+"-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				seed := models.User{Username: username, Password: string(hash), Role: models.Proctor}
				if err := models.DB.Create(&seed).Error; err != nil {
					t.Fatalf("failed to seed user %s: %v", username, err)
				}
			}

			w := performListUsersRequest(t)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			for _, part := range tc.expectedContains {
				if !strings.Contains(w.Body.String(), part) {
					t.Fatalf("expected body to contain %q, got body=%s", part, w.Body.String())
				}
			}

			for _, part := range tc.expectedNotContain {
				if strings.Contains(w.Body.String(), part) {
					t.Fatalf("expected body not to contain %q, got body=%s", part, w.Body.String())
				}
			}

			var resp struct {
				Success bool          `json:"success"`
				Data    []models.User `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if !resp.Success {
				t.Fatalf("expected success=true, got false")
			}
			if len(resp.Data) != tc.expectedDataCount {
				t.Fatalf("expected %d users, got %d", tc.expectedDataCount, len(resp.Data))
			}
		})
	}
}

func TestDeleteUser(t *testing.T) {
	testCases := []struct {
		name                 string
		seedUser             *models.User
		targetID             string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		expectDeleted        bool
	}{
		{
			name: "delete user success",
			seedUser: &models.User{
				Username: "alice",
				Role:     models.Proctor,
			},
			targetID:             "1",
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			expectDeleted:        true,
		},
		{
			name: "cannot delete admin user",
			seedUser: &models.User{
				Username: "admin",
				Role:     models.Admin,
			},
			targetID:             "1",
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "用户不存在或无法删除",
			expectSuccess:        false,
			expectDeleted:        false,
		},
		{
			name:                 "user not found",
			targetID:             "999",
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "用户不存在或无法删除",
			expectSuccess:        false,
			expectDeleted:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			if tc.seedUser != nil {
				hash, err := bcrypt.GenerateFromPassword([]byte("seed-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				user := *tc.seedUser
				user.Password = string(hash)
				if err := models.DB.Create(&user).Error; err != nil {
					t.Fatalf("failed to seed user: %v", err)
				}
			}

			w := performDeleteUserRequest(t, tc.targetID)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}

			if tc.seedUser != nil {
				var normalQuery models.User
				err := models.DB.Where("id = ?", 1).First(&normalQuery).Error
				if tc.expectDeleted {
					if err == nil {
						t.Fatalf("expected user to be deleted")
					}

					var deletedUser models.User
					if err := models.DB.Unscoped().Where("id = ?", 1).First(&deletedUser).Error; err == nil {
						t.Fatalf("expected hard delete, but user still exists in unscoped query")
					}
				} else {
					if err != nil {
						t.Fatalf("expected user to remain, got err=%v", err)
					}
				}
			}
		})
	}
}

func TestUpdateUser(t *testing.T) {
	testCases := []struct {
		name                 string
		seedUsers            []models.User
		targetID             string
		requestBody          string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name: "partial update username success",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"username":"alice_new"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var user models.User
				if err := models.DB.First(&user, 1).Error; err != nil {
					t.Fatalf("expected user to exist, got err=%v", err)
				}
				if user.Username != "alice_new" {
					t.Fatalf("expected username alice_new, got %s", user.Username)
				}
			},
		},
		{
			name: "partial update password success",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"password":"new-pass-123"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var user models.User
				if err := models.DB.First(&user, 1).Error; err != nil {
					t.Fatalf("expected user to exist, got err=%v", err)
				}
				if user.Password == "new-pass-123" {
					t.Fatalf("expected password to be hashed, got plain text")
				}
				if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("new-pass-123")); err != nil {
					t.Fatalf("stored hash does not match password: %v", err)
				}
			},
		},
		{
			name: "update role success",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"role":"admin"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var user models.User
				if err := models.DB.First(&user, 1).Error; err != nil {
					t.Fatalf("expected user to exist, got err=%v", err)
				}
				if user.Role != models.Admin {
					t.Fatalf("expected role admin, got %s", user.Role)
				}
			},
		},
		{
			name: "invalid role",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"role":"guest"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "角色非法",
			expectSuccess:        false,
		},
		{
			name: "empty username",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"username":"   "}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "用户名不能为空",
			expectSuccess:        false,
		},
		{
			name: "empty password",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"password":""}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "密码不能为空",
			expectSuccess:        false,
		},
		{
			name: "no updatable fields",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "没有可更新字段",
			expectSuccess:        false,
		},
		{
			name: "duplicate username",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
				{Username: "bob", Role: models.Proctor},
			},
			targetID:             "1",
			requestBody:          `{"username":"bob"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "用户名已被他人占用",
			expectSuccess:        false,
		},
		{
			name: "user not found",
			seedUsers: []models.User{
				{Username: "alice", Role: models.Proctor},
			},
			targetID:             "999",
			requestBody:          `{"username":"who"}`,
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "用户不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			for _, seedUser := range tc.seedUsers {
				hash, err := bcrypt.GenerateFromPassword([]byte(seedUser.Username+"-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				user := seedUser
				user.Password = string(hash)
				if err := models.DB.Create(&user).Error; err != nil {
					t.Fatalf("failed to seed user %s: %v", seedUser.Username, err)
				}
			}

			w := performUpdateUserRequest(t, tc.targetID, tc.requestBody)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}

			if tc.verify != nil {
				tc.verify(t)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	testCases := []struct {
		name                 string
		sessionUserID        any
		seedUser             *models.User
		requestBody          string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:                 "unauthorized when no session",
			requestBody:          `{"old_password":"old-pass","new_password":"new-pass"}`,
			expectedCode:         http.StatusUnauthorized,
			expectedBodyContains: "用户未登录",
			expectSuccess:        false,
		},
		{
			name:                 "unauthorized when invalid session user id",
			sessionUserID:        "abc",
			requestBody:          `{"old_password":"old-pass","new_password":"new-pass"}`,
			expectedCode:         http.StatusUnauthorized,
			expectedBodyContains: "用户未登录",
			expectSuccess:        false,
		},
		{
			name:                 "invalid input",
			sessionUserID:        uint(1),
			requestBody:          `{"old_password":"old-pass"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "输入错误",
			expectSuccess:        false,
		},
		{
			name:          "user not found",
			sessionUserID: uint(999),
			requestBody:   `{"old_password":"old-pass","new_password":"new-pass"}`,
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "用户不存在",
			expectSuccess:        false,
		},
		{
			name:          "old password incorrect",
			sessionUserID: uint(1),
			seedUser: &models.User{
				Username: "alice",
				Role:     models.Proctor,
			},
			requestBody:          `{"old_password":"wrong-old","new_password":"new-pass"}`,
			expectedCode:         http.StatusUnauthorized,
			expectedBodyContains: "旧密码错误",
			expectSuccess:        false,
		},
		{
			name:          "change password success",
			sessionUserID: uint(1),
			seedUser: &models.User{
				Username: "alice",
				Role:     models.Proctor,
			},
			requestBody:          `{"old_password":"old-pass","new_password":"new-pass-123"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var user models.User
				if err := models.DB.First(&user, 1).Error; err != nil {
					t.Fatalf("expected user to exist, got err=%v", err)
				}
				if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte("new-pass-123")); err != nil {
					t.Fatalf("expected password to be updated, got err=%v", err)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupUsersHandlerTestDB(t)
			defer cleanup()

			if tc.seedUser != nil {
				hash, err := bcrypt.GenerateFromPassword([]byte("old-pass"), bcrypt.DefaultCost)
				if err != nil {
					t.Fatalf("failed to hash seed password: %v", err)
				}
				user := *tc.seedUser
				user.Password = string(hash)
				if err := models.DB.Create(&user).Error; err != nil {
					t.Fatalf("failed to seed user: %v", err)
				}
			}

			w := performChangePasswordRequest(t, tc.sessionUserID, tc.requestBody)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool `json:"success"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}

			if tc.verify != nil {
				tc.verify(t)
			}
		})
	}
}
