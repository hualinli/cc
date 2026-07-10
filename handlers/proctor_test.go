package handlers

import (
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupProctorTestDB(t *testing.T) func() {
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
	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	models.DB = db
	return func() {
		_ = db.Migrator().DropTable(&models.Exam{}, &models.Node{}, &models.Room{}, &models.User{})
		_ = sqlDB.Close()
		models.DB = oldDB
	}
}

func seedProctorUser(t *testing.T) models.User {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.DefaultCost)
	user := models.User{Username: "proctor1", Password: string(hash), Role: models.Proctor}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return user
}

func seedProctorRoom(t *testing.T) models.Room {
	t.Helper()
	r := models.Room{Name: "A101", Building: "Main", RTSPUrl: "rtsp://x"}
	if err := models.DB.Create(&r).Error; err != nil {
		t.Fatalf("seed room: %v", err)
	}
	return r
}

func seedProctorNode(t *testing.T) models.Node {
	t.Helper()
	n := models.Node{Name: "node-1", Token: "tok", Status: models.NodeStatusIdle, Address: "10.0.0.1:8080"}
	if err := models.DB.Create(&n).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return n
}

func performProctorJSONRequest(t *testing.T, r *gin.Engine, method, path string, sessionUserID any, sessionRole any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if sessionUserID != nil || sessionRole != nil {
		// Create session
		store := cookie.NewStore([]byte("test-secret"))
		sessionR := gin.New()
		sessionR.Use(sessions.Sessions("test-session", store))
		sessionR.Use(func(c *gin.Context) {
			s := sessions.Default(c)
			if sessionUserID != nil {
				s.Set("user_id", sessionUserID)
			}
			if sessionRole != nil {
				s.Set("role", sessionRole)
			}
			_ = s.Save()
			c.Next()
		})
		sessionR.GET("/_set", func(c *gin.Context) {})
		sReq := httptest.NewRequest(http.MethodGet, "/_set", nil)
		sW := httptest.NewRecorder()
		sessionR.ServeHTTP(sW, sReq)
		cookie := sW.Header().Get("Set-Cookie")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func setupProctorRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	r.Use(sessions.Sessions("test-session", store))
	r.GET("/api/proctor/exams", ListProctorExams)
	r.GET("/api/proctor/exams/stats", GetProctorExamStats)
	return r
}

func TestListProctorExams_Unauthorized(t *testing.T) {
	cleanup := setupProctorTestDB(t)
	defer cleanup()
	r := setupProctorRouter()
	w := performProctorJSONRequest(t, r, http.MethodGet, "/api/proctor/exams", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListProctorExams_Pagination(t *testing.T) {
	cleanup := setupProctorTestDB(t)
	defer cleanup()
	user := seedProctorUser(t)
	room := seedProctorRoom(t)
	_ = seedProctorNode(t)

	now := time.Now()
	for i := 0; i < 25; i++ {
		e := models.Exam{
			Name:           "exam-" + string(rune('A'+i%26)),
			Subject:        "math",
			RoomID:         room.ID,
			UserID:         user.ID,
			StartTime:      now.Add(time.Duration(i) * time.Hour),
			ScheduleStatus: models.ExamScheduleRunning,
		}
		if err := models.DB.Create(&e).Error; err != nil {
			t.Fatalf("seed exam %d: %v", i, err)
		}
	}

	r := setupProctorRouter()
	w := performProctorJSONRequest(t, r, http.MethodGet, "/api/proctor/exams?page=1&page_size=20", user.ID, "proctor")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success    bool `json:"success"`
		Pagination struct {
			Page      int   `json:"page"`
			PageSize  int   `json:"page_size"`
			Total     int64 `json:"total"`
			TotalPage int64 `json:"total_page"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
	if resp.Pagination.Total != 25 {
		t.Fatalf("expected total 25, got %d", resp.Pagination.Total)
	}
	if resp.Pagination.PageSize != 20 {
		t.Fatalf("expected page_size 20, got %d", resp.Pagination.PageSize)
	}
}

func TestGetProctorExamStats(t *testing.T) {
	cleanup := setupProctorTestDB(t)
	defer cleanup()
	user := seedProctorUser(t)
	room := seedProctorRoom(t)
	_ = seedProctorNode(t)

	now := time.Now()
	// ongoing exam
	e1 := models.Exam{
		Name: "ongoing", Subject: "math", RoomID: room.ID, UserID: user.ID,
		StartTime: now.Add(-1 * time.Hour), ScheduleStatus: models.ExamScheduleRunning,
	}
	models.DB.Create(&e1)
	// upcoming exam
	e2 := models.Exam{
		Name: "upcoming", Subject: "phys", RoomID: room.ID, UserID: user.ID,
		StartTime: now.Add(1 * time.Hour), ScheduleStatus: models.ExamSchedulePending,
	}
	models.DB.Create(&e2)
	// completed exam
	endTime := now.Add(-30 * time.Minute)
	e3 := models.Exam{
		Name: "completed", Subject: "hist", RoomID: room.ID, UserID: user.ID,
		StartTime: now.Add(-2 * time.Hour), EndTime: &endTime, ScheduleStatus: models.ExamScheduleRunning,
	}
	models.DB.Create(&e3)

	r := setupProctorRouter()
	w := performProctorJSONRequest(t, r, http.MethodGet, "/api/proctor/exams/stats", user.ID, "proctor")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Total     int64 `json:"total"`
			Ongoing   int64 `json:"ongoing"`
			Upcoming  int64 `json:"upcoming"`
			Completed int64 `json:"completed"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.Total != 3 {
		t.Fatalf("expected total 3, got %d", resp.Data.Total)
	}
	if resp.Data.Ongoing != 1 {
		t.Fatalf("expected ongoing 1, got %d", resp.Data.Ongoing)
	}
	if resp.Data.Upcoming != 1 {
		t.Fatalf("expected upcoming 1, got %d", resp.Data.Upcoming)
	}
	if resp.Data.Completed != 1 {
		t.Fatalf("expected completed 1, got %d", resp.Data.Completed)
	}
}

func TestListProctorExams_StatusFilter(t *testing.T) {
	cleanup := setupProctorTestDB(t)
	defer cleanup()
	user := seedProctorUser(t)
	room := seedProctorRoom(t)
	_ = seedProctorNode(t)

	now := time.Now()
	// ongoing
	models.DB.Create(&models.Exam{
		Name: "ongoing", Subject: "math", RoomID: room.ID, UserID: user.ID,
		StartTime: now.Add(-1 * time.Hour), ScheduleStatus: models.ExamScheduleRunning,
	})
	// upcoming
	models.DB.Create(&models.Exam{
		Name: "upcoming", Subject: "phys", RoomID: room.ID, UserID: user.ID,
		StartTime: now.Add(1 * time.Hour), ScheduleStatus: models.ExamSchedulePending,
	})

	r := setupProctorRouter()
	w := performProctorJSONRequest(t, r, http.MethodGet, "/api/proctor/exams?status=ongoing", user.ID, "proctor")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"ongoing"`) {
		t.Fatalf("expected ongoing in result, got %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"upcoming"`) {
		t.Fatal("expected no upcoming with status=ongoing filter")
	}
}
