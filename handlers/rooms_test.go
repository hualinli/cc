package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRoomsHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
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

func seedRoom(t *testing.T, name string, building string, rtsp string) models.Room {
	t.Helper()
	room := models.Room{Name: name, Building: building, RTSPUrl: rtsp}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedUser(t *testing.T, username string) models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("seed-pass"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash seed password: %v", err)
	}
	user := models.User{Username: username, Password: string(hash), Role: models.Proctor}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedExamForRoom(t *testing.T, roomID uint, userID uint) models.Exam {
	t.Helper()
	exam := models.Exam{
		Name:           "exam-1",
		Subject:        "subject-1",
		RoomID:         roomID,
		UserID:         userID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamSchedulePending,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}
	return exam
}

func performCreateRoomRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/rooms", CreateRoom)

	req := httptest.NewRequest(http.MethodPost, "/rooms", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performGetRoomRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/rooms/:id", GetRoom)

	req := httptest.NewRequest(http.MethodGet, "/rooms/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performListRoomsRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/rooms", ListRooms)

	req := httptest.NewRequest(http.MethodGet, "/rooms", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performUpdateRoomRequest(t *testing.T, id string, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/rooms/:id", UpdateRoom)

	req := httptest.NewRequest(http.MethodPut, "/rooms/"+id, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func performDeleteRoomRequest(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/rooms/:id", DeleteRoom)

	req := httptest.NewRequest(http.MethodDelete, "/rooms/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateRoom(t *testing.T) {
	testCases := []struct {
		name                 string
		requestBody          string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:                 "create room success",
			requestBody:          `{"name":"A101","building":"Main","rtsp_url":"rtsp://cam/1"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var room models.Room
				if err := models.DB.Where("name = ?", "A101").First(&room).Error; err != nil {
					t.Fatalf("expected room in db, got err=%v", err)
				}
			},
		},
		{
			name:                 "invalid input missing name",
			requestBody:          `{"building":"Main","rtsp_url":"rtsp://cam/1"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "输入错误",
			expectSuccess:        false,
		},
		{
			name:                 "empty value after trim",
			requestBody:          `{"name":"   ","building":"Main","rtsp_url":"rtsp://cam/1"}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "教室名称、楼栋和RTSP地址不能为空",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupRoomsHandlerTestDB(t)
			defer cleanup()

			w := performCreateRoomRequest(t, tc.requestBody)
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

func TestGetRoom(t *testing.T) {
	testCases := []struct {
		name                 string
		seed                 bool
		id                   string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
	}{
		{
			name:                 "get room success",
			seed:                 true,
			id:                   "1",
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"name":"A101"`,
			expectSuccess:        true,
		},
		{
			name:                 "room not found",
			id:                   "999",
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "教室不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupRoomsHandlerTestDB(t)
			defer cleanup()

			if tc.seed {
				seedRoom(t, "A101", "Main", "rtsp://cam/1")
			}

			w := performGetRoomRequest(t, tc.id)
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

func TestListRooms(t *testing.T) {
	testCases := []struct {
		name               string
		seedRooms          bool
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
			name:               "list with rooms",
			seedRooms:          true,
			expectedCode:       http.StatusOK,
			expectedDataCount:  2,
			expectedContains:   []string{`"name":"A101"`, `"name":"B202"`, `"success":true`},
			expectedNotContain: []string{"password"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupRoomsHandlerTestDB(t)
			defer cleanup()

			if tc.seedRooms {
				seedRoom(t, "A101", "Main", "rtsp://cam/1")
				seedRoom(t, "B202", "East", "rtsp://cam/2")
			}

			w := performListRoomsRequest(t)
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
				Data    []models.Room `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if !resp.Success {
				t.Fatalf("expected success=true, got false")
			}
			if len(resp.Data) != tc.expectedDataCount {
				t.Fatalf("expected %d rooms, got %d", tc.expectedDataCount, len(resp.Data))
			}
		})
	}
}

func TestUpdateRoom(t *testing.T) {
	testCases := []struct {
		name                 string
		seed                 bool
		id                   string
		requestBody          string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:                 "update room success",
			seed:                 true,
			id:                   "1",
			requestBody:          `{"name":"A102"}`,
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var room models.Room
				if err := models.DB.First(&room, 1).Error; err != nil {
					t.Fatalf("expected room in db, got err=%v", err)
				}
				if room.Name != "A102" {
					t.Fatalf("expected name A102, got %s", room.Name)
				}
			},
		},
		{
			name:                 "invalid empty building",
			seed:                 true,
			id:                   "1",
			requestBody:          `{"building":"   "}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "楼栋不能为空",
			expectSuccess:        false,
		},
		{
			name:                 "no update fields",
			seed:                 true,
			id:                   "1",
			requestBody:          `{}`,
			expectedCode:         http.StatusBadRequest,
			expectedBodyContains: "没有提供有效的更新字段",
			expectSuccess:        false,
		},
		{
			name:                 "room not found",
			id:                   "999",
			requestBody:          `{"name":"A102"}`,
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "教室不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupRoomsHandlerTestDB(t)
			defer cleanup()

			if tc.seed {
				seedRoom(t, "A101", "Main", "rtsp://cam/1")
			}

			w := performUpdateRoomRequest(t, tc.id, tc.requestBody)
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

func TestDeleteRoom(t *testing.T) {
	testCases := []struct {
		name                 string
		seedRoom             bool
		seedExam             bool
		id                   string
		expectedCode         int
		expectedBodyContains string
		expectSuccess        bool
		verify               func(t *testing.T)
	}{
		{
			name:                 "delete room success",
			seedRoom:             true,
			id:                   "1",
			expectedCode:         http.StatusOK,
			expectedBodyContains: `"success":true`,
			expectSuccess:        true,
			verify: func(t *testing.T) {
				var room models.Room
				if err := models.DB.Unscoped().Where("id = ?", 1).First(&room).Error; err == nil {
					t.Fatalf("expected room to be hard deleted")
				}
			},
		},
		{
			name:                 "delete room foreign key conflict",
			seedRoom:             true,
			seedExam:             true,
			id:                   "1",
			expectedCode:         http.StatusConflict,
			expectedBodyContains: "无法删除教室：存在关联考试记录",
			expectSuccess:        false,
			verify: func(t *testing.T) {
				var room models.Room
				if err := models.DB.Where("id = ?", 1).First(&room).Error; err != nil {
					t.Fatalf("expected room to remain, got err=%v", err)
				}
			},
		},
		{
			name:                 "room not found",
			id:                   "999",
			expectedCode:         http.StatusNotFound,
			expectedBodyContains: "教室不存在",
			expectSuccess:        false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupRoomsHandlerTestDB(t)
			defer cleanup()

			if tc.seedRoom {
				room := seedRoom(t, "A101", "Main", "rtsp://cam/1")
				if tc.seedExam {
					user := seedUser(t, "proctor1")
					_ = seedExamForRoom(t, room.ID, user.ID)
				}
			}

			w := performDeleteRoomRequest(t, tc.id)
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
