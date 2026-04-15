package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupNodesHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		_ = db.Migrator().DropTable(&models.Exam{}, &models.Node{}, &models.Room{}, &models.User{})
		if closeErr := sqlDB.Close(); closeErr != nil {
			t.Fatalf("failed to close sql db: %v", closeErr)
		}
		models.DB = oldDB
	}
}

func seedNode(t *testing.T, name string, model string, address string, status string, currentUserID *uint) models.Node {
	t.Helper()

	node := models.Node{
		Name:          name,
		Token:         generateToken(),
		NodeModel:     model,
		Address:       address,
		Status:        status,
		Version:       "1.0.0",
		CurrentUserID: currentUserID,
	}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

func seedNodeUser(t *testing.T, username string, role models.UserRole) models.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("seed-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	user := models.User{Username: username, Password: string(hash), Role: role}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedNodeRoom(t *testing.T) models.Room {
	t.Helper()

	room := models.Room{Name: "A101", Building: "Main", RTSPUrl: "rtsp://cam/1"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedExamForNode(t *testing.T, roomID uint, userID uint, nodeID uint) models.Exam {
	t.Helper()

	examNodeID := nodeID
	exam := models.Exam{
		Name:           "exam-1",
		Subject:        "subject-1",
		RoomID:         roomID,
		NodeID:         &examNodeID,
		UserID:         userID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamSchedulePending,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}
	return exam
}

func performCreateNodeRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/nodes", CreateNode)

	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performDeleteNodeRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/nodes/:id", DeleteNode)

	req := httptest.NewRequest(http.MethodDelete, "/nodes/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performUpdateNodeRequest(t *testing.T, id string, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/nodes/:id", UpdateNode)

	req := httptest.NewRequest(http.MethodPut, "/nodes/"+id, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performGetNodeRequestWithSession(t *testing.T, id string, sessionUserID any, sessionRole any) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		if sessionUserID != nil {
			session.Set("user_id", sessionUserID)
		}
		if sessionRole != nil {
			session.Set("role", sessionRole)
		}
		if err := session.Save(); err != nil {
			t.Fatalf("failed to save test session: %v", err)
		}
		c.Next()
	})
	r.GET("/nodes/:id", GetNode)

	req := httptest.NewRequest(http.MethodGet, "/nodes/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performGetNodeJumpURLRequestWithSession(t *testing.T, id string, sessionUserID any, sessionRole any) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		if sessionUserID != nil {
			session.Set("user_id", sessionUserID)
		}
		if sessionRole != nil {
			session.Set("role", sessionRole)
		}
		if err := session.Save(); err != nil {
			t.Fatalf("failed to save test session: %v", err)
		}
		c.Next()
	})
	r.GET("/nodes/:id/jump", GetNodeJumpURL)

	req := httptest.NewRequest(http.MethodGet, "/nodes/"+id+"/jump", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performListNodesRequestWithSession(t *testing.T, sessionUserID any, sessionRole any) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		if sessionUserID != nil {
			session.Set("user_id", sessionUserID)
		}
		if sessionRole != nil {
			session.Set("role", sessionRole)
		}
		if err := session.Save(); err != nil {
			t.Fatalf("failed to save test session: %v", err)
		}
		c.Next()
	})
	r.GET("/nodes", ListNodes)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateNode(t *testing.T) {
	testCases := []struct {
		name                 string
		requestBody          string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:                 "create node success with address",
			requestBody:          `{"name":"node-1","model":"m1","address":"10.0.0.1:8080"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var node models.Node
				if err := models.DB.Where("name = ?", "node-1").First(&node).Error; err != nil {
					t.Fatalf("expected node in db, got err=%v", err)
				}
				if node.Address != "10.0.0.1:8080" {
					t.Fatalf("expected address 10.0.0.1:8080, got %s", node.Address)
				}
			},
		},
		{
			name:                 "create node default address for whitespace",
			requestBody:          `{"name":"node-2","model":"m2","address":"   "}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var node models.Node
				if err := models.DB.Where("name = ?", "node-2").First(&node).Error; err != nil {
					t.Fatalf("expected node in db, got err=%v", err)
				}
				if node.Address != "waiting_for_heartbeat" {
					t.Fatalf("expected default address waiting_for_heartbeat, got %s", node.Address)
				}
			},
		},
		{
			name:                 "invalid input missing name",
			requestBody:          `{"model":"m1"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "输入错误",
			expectSuccess:        false,
		},
		{
			name:                 "invalid input blank name after trim",
			requestBody:          `{"name":"   ","model":"m1"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "节点名称和模型不能为空",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			w := performCreateNodeRequest(t, tc.requestBody)
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

func TestDeleteNode(t *testing.T) {
	testCases := []struct {
		name                 string
		id                   string
		setup                func(t *testing.T)
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name: "delete node success",
			id:   "1",
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var node models.Node
				if err := models.DB.Unscoped().Where("id = ?", 1).First(&node).Error; err == nil {
					t.Fatalf("expected node to be hard deleted")
				}
			},
		},
		{
			name: "delete node occupied",
			id:   "1",
			setup: func(t *testing.T) {
				user := seedNodeUser(t, "proctor1", models.Proctor)
				_ = seedNode(t, "node-occupied", "m1", "10.0.0.2:8080", models.NodeStatusBusy, &user.ID)
			},
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "无法删除节点：该节点当前正被监考员占用",
			expectSuccess:        false,
		},
		{
			name: "delete node foreign key conflict",
			id:   "1",
			setup: func(t *testing.T) {
				node := seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
				if err := models.DB.Exec(`CREATE TABLE node_refs (
					id INTEGER PRIMARY KEY,
					node_id INTEGER NOT NULL,
					FOREIGN KEY(node_id) REFERENCES nodes(id) ON DELETE RESTRICT
				);`).Error; err != nil {
					t.Fatalf("failed to create node_refs table: %v", err)
				}
				if err := models.DB.Exec("INSERT INTO node_refs(node_id) VALUES (?)", node.ID).Error; err != nil {
					t.Fatalf("failed to insert node_refs row: %v", err)
				}
			},
			expectedCode:         http.StatusConflict,
			expectedBodyContains: "无法删除节点：存在关联考试记录",
			expectSuccess:        false,
			verify: func(t *testing.T) {
				var node models.Node
				if err := models.DB.Where("id = ?", 1).First(&node).Error; err != nil {
					t.Fatalf("expected node to remain, got err=%v", err)
				}
			},
		},
		{
			name:                 "node not found",
			id:                   "999",
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "节点不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			if tc.setup != nil {
				tc.setup(t)
			}

			w := performDeleteNodeRequest(t, tc.id)
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

func TestUpdateNode(t *testing.T) {
	testCases := []struct {
		name                 string
		id                   string
		requestBody          string
		setup                func(t *testing.T)
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:        "update node success",
			id:          "1",
			requestBody: `{"name":"node-1-updated","address":"10.0.0.2:8080"}`,
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var node models.Node
				if err := models.DB.First(&node, 1).Error; err != nil {
					t.Fatalf("expected node in db, got err=%v", err)
				}
				if node.Name != "node-1-updated" {
					t.Fatalf("expected updated name, got %s", node.Name)
				}
				if node.Address != "10.0.0.2:8080" {
					t.Fatalf("expected updated address, got %s", node.Address)
				}
			},
		},
		{
			name:        "invalid empty name",
			id:          "1",
			requestBody: `{"name":"   "}`,
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "节点名称不能为空",
			expectSuccess:        false,
		},
		{
			name:        "no update fields",
			id:          "1",
			requestBody: `{}`,
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "没有提供有效的更新字段",
			expectSuccess:        false,
		},
		{
			name:                 "node not found",
			id:                   "999",
			requestBody:          `{"name":"node-1-updated"}`,
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "节点不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			if tc.setup != nil {
				tc.setup(t)
			}

			w := performUpdateNodeRequest(t, tc.id, tc.requestBody)
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

func TestGetNode(t *testing.T) {
	testCases := []struct {
		name                 string
		id                   string
		sessionUserID        any
		sessionRole          any
		setup                func(t *testing.T)
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
	}{
		{
			name:          "admin get node success",
			id:            "1",
			sessionUserID: uint(1),
			sessionRole:   "admin",
			setup: func(t *testing.T) {
				other := seedNodeUser(t, "proctor2", models.Proctor)
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusBusy, &other.ID)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"name":"node-1"`,
			expectSuccess:        true,
		},
		{
			name:          "proctor forbidden for others occupied node",
			id:            "1",
			sessionUserID: uint(999),
			sessionRole:   "proctor",
			setup: func(t *testing.T) {
				other := seedNodeUser(t, "proctor2", models.Proctor)
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusBusy, &other.ID)
			},
			expectedCode:         http.StatusForbidden,
			expectedBodyContains: "无权访问此节点",
			expectSuccess:        false,
		},
		{
			name:          "proctor get own occupied node success",
			id:            "1",
			sessionUserID: uint(1),
			sessionRole:   "proctor",
			setup: func(t *testing.T) {
				owner := seedNodeUser(t, "proctor1", models.Proctor)
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusBusy, &owner.ID)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"name":"node-1"`,
			expectSuccess:        true,
		},
		{
			name:                 "session role missing",
			id:                   "1",
			sessionUserID:        uint(1),
			setup:                func(t *testing.T) { _ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil) },
			expectedCode:         http.StatusForbidden,
			expectedBodyContains: "权限不足",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			if tc.setup != nil {
				tc.setup(t)
			}

			w := performGetNodeRequestWithSession(t, tc.id, tc.sessionUserID, tc.sessionRole)
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
		})
	}
}

func TestGetNodeJumpURL(t *testing.T) {
	testCases := []struct {
		name                 string
		id                   string
		sessionUserID        any
		sessionRole          any
		setup                func(t *testing.T)
		expectedCode         int
		expectedBodyContains string
		expectedURLPrefix    string
		expectSuccess        bool
	}{
		{
			name:          "admin jump url success",
			id:            "1",
			sessionUserID: uint(1),
			sessionRole:   "admin",
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectedURLPrefix:    "http://10.0.0.1:8080?token=",
			expectSuccess:        true,
		},
		{
			name:          "jump missing role",
			id:            "1",
			sessionUserID: uint(1),
			setup: func(t *testing.T) {
				_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
			},
			expectedCode:         http.StatusForbidden,
			expectedBodyContains: "获取用户角色失败",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			if tc.setup != nil {
				tc.setup(t)
			}

			w := performGetNodeJumpURLRequestWithSession(t, tc.id, tc.sessionUserID, tc.sessionRole)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			if !strings.Contains(w.Body.String(), tc.expectedBodyContains) {
				t.Fatalf("expected body to contain %q, got body=%s", tc.expectedBodyContains, w.Body.String())
			}

			var resp struct {
				Success bool   `json:"success"`
				JumpURL string `json:"jump_url"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}
			if tc.expectedURLPrefix != "" && !strings.HasPrefix(resp.JumpURL, tc.expectedURLPrefix) {
				t.Fatalf("expected jump url prefix %q, got %q", tc.expectedURLPrefix, resp.JumpURL)
			}
		})
	}
}

func TestGetNodeJumpURLProctorAcquireNode(t *testing.T) {
	cleanup := setupNodesHandlerTestDB(t)
	defer cleanup()

	user := seedNodeUser(t, "proctor1", models.Proctor)
	_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)

	w := performGetNodeJumpURLRequestWithSession(t, "1", user.ID, "proctor")
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool   `json:"success"`
		JumpURL string `json:"jump_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got body=%s", w.Body.String())
	}
	if !strings.HasPrefix(resp.JumpURL, "http://10.0.0.1:8080?token=") {
		t.Fatalf("expected jump_url prefix, got %q", resp.JumpURL)
	}
}

func TestGetNodeJumpURLConcurrentAccess(t *testing.T) {
	cleanup := setupNodesHandlerTestDB(t)
	defer cleanup()

	_ = seedNode(t, "node-1", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
	_ = seedNode(t, "node-2", "m2", "10.0.0.2:8080", models.NodeStatusIdle, nil)

	var successCount int32
	var forbiddenCount int32
	var wg sync.WaitGroup
	wg.Add(10)

	for i := 0; i < 10; i++ {
		user := seedNodeUser(t, fmt.Sprintf("proctor-%d", i+1), models.Proctor)
		nodeID := "1"
		if i >= 5 {
			nodeID = "2"
		}
		go func(uid uint, nid string) {
			defer wg.Done()
			w := performGetNodeJumpURLRequestWithSession(t, nid, uid, "proctor")
			switch w.Code {
			case http.StatusOK:
				atomic.AddInt32(&successCount, 1)
			case http.StatusForbidden:
				atomic.AddInt32(&forbiddenCount, 1)
			default:
				t.Errorf("unexpected status %d, body=%s", w.Code, w.Body.String())
			}
		}(user.ID, nodeID)
	}

	wg.Wait()

	if successCount != 2 || forbiddenCount != 8 {
		t.Fatalf("expected two successes and eight forbidden, got success=%d forbidden=%d", successCount, forbiddenCount)
	}
}

func TestListNodes(t *testing.T) {
	testCases := []struct {
		name                 string
		sessionUserID        any
		sessionRole          any
		setup                func(t *testing.T)
		expectedCode         int
		expectedBodyContains []string
		expectedNotContains  []string
		expectedDataCount    int
		expectSuccess        bool
	}{
		{
			name:          "admin can list all nodes",
			sessionUserID: uint(1),
			sessionRole:   "admin",
			setup: func(t *testing.T) {
				owner1 := seedNodeUser(t, "proctor1", models.Proctor)
				owner2 := seedNodeUser(t, "proctor2", models.Proctor)
				_ = seedNode(t, "node-free", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
				_ = seedNode(t, "node-own", "m1", "10.0.0.2:8080", models.NodeStatusBusy, &owner1.ID)
				_ = seedNode(t, "node-other", "m1", "10.0.0.3:8080", models.NodeStatusBusy, &owner2.ID)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: []string{`"node-free"`, `"node-own"`, `"node-other"`},
			expectedDataCount:    3,
			expectSuccess:        true,
		},
		{
			name:          "proctor sees free and own only",
			sessionUserID: uint(1),
			sessionRole:   "proctor",
			setup: func(t *testing.T) {
				owner1 := seedNodeUser(t, "proctor1", models.Proctor)
				owner2 := seedNodeUser(t, "proctor2", models.Proctor)
				_ = seedNode(t, "node-free", "m1", "10.0.0.1:8080", models.NodeStatusIdle, nil)
				_ = seedNode(t, "node-own", "m1", "10.0.0.2:8080", models.NodeStatusBusy, &owner1.ID)
				_ = seedNode(t, "node-other", "m1", "10.0.0.3:8080", models.NodeStatusBusy, &owner2.ID)
			},
			expectedCode:         http.StatusOK,
			expectedBodyContains: []string{`"node-free"`, `"node-own"`},
			expectedNotContains:  []string{`"node-other"`},
			expectedDataCount:    2,
			expectSuccess:        true,
		},
		{
			name:                 "session role missing",
			sessionUserID:        uint(1),
			expectedCode:         http.StatusForbidden,
			expectedBodyContains: []string{"权限不足"},
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupNodesHandlerTestDB(t)
			defer cleanup()

			if tc.setup != nil {
				tc.setup(t)
			}

			w := performListNodesRequestWithSession(t, tc.sessionUserID, tc.sessionRole)
			if w.Code != tc.expectedCode {
				t.Fatalf("expected status %d, got %d, body=%s", tc.expectedCode, w.Code, w.Body.String())
			}

			for _, part := range tc.expectedBodyContains {
				if !strings.Contains(w.Body.String(), part) {
					t.Fatalf("expected body to contain %q, got body=%s", part, w.Body.String())
				}
			}
			for _, part := range tc.expectedNotContains {
				if strings.Contains(w.Body.String(), part) {
					t.Fatalf("expected body not to contain %q, got body=%s", part, w.Body.String())
				}
			}

			var resp struct {
				Success bool          `json:"success"`
				Data    []models.Node `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if resp.Success != tc.expectSuccess {
				t.Fatalf("expected success=%v, got %v", tc.expectSuccess, resp.Success)
			}
			if tc.expectedDataCount != 0 || tc.expectSuccess {
				if len(resp.Data) != tc.expectedDataCount {
					t.Fatalf("expected %d nodes, got %d", tc.expectedDataCount, len(resp.Data))
				}
			}
		})
	}
}
