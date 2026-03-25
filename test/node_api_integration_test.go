package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cc/handlers"
	"cc/middleware"
	"cc/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupNodeApiRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	nodeAPI := r.Group("/node-api/v1")
	nodeAPI.Use(middleware.NodeAuthMiddleware())
	{
		nodeAPI.POST("/heartbeat", handlers.NodeHeartbeat)
		nodeAPI.POST("/tasks/sync", handlers.SyncTask)
		nodeAPI.POST("/alerts", handlers.ReportAlert)
	}
	return r
}

func setupNodeTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test database: %v", err)
	}
	db.AutoMigrate(&models.Node{}, &models.Exam{}, &models.Alert{}, &models.User{}, &models.Room{})
	models.DB = db
	return db
}

func TestNodeHeartbeat(t *testing.T) {
	db := setupNodeTestDB(t)
	node := models.Node{Name: "TestNode", Token: "test-token", Status: models.NodeStatusIdle}
	db.Create(&node)
	r := setupNodeApiRouter()

	input := map[string]string{"status": models.NodeStatusBusy}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/node-api/v1/heartbeat", bytes.NewBuffer(body))
	req.Header.Set("X-Node-Token", "test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestSyncTaskStart(t *testing.T) {
	db := setupNodeTestDB(t)
	room := models.Room{Name: "Room 1", Building: "A", RTSPUrl: "rtsp://example.com/stream"}
	db.Create(&room)
	userID := uint(1)
	node := models.Node{Name: "TestNode", Token: "test-token", CurrentUserID: &userID, Status: models.NodeStatusIdle}
	db.Create(&node)
	r := setupNodeApiRouter()

	startInput := map[string]any{
		"action":         "start",
		"room_id":        1,
		"subject":        "Math",
		"start_time":     time.Now(),
		"examinee_count": 50,
	}
	body, _ := json.Marshal(startInput)
	req := httptest.NewRequest("POST", "/node-api/v1/tasks/sync", bytes.NewBuffer(body))
	req.Header.Set("X-Node-Token", "test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestSyncTaskStart_IdempotentOnSameNode(t *testing.T) {
	db := setupNodeTestDB(t)
	room := models.Room{Name: "Room 2", Building: "A", RTSPUrl: "rtsp://example.com/stream2"}
	db.Create(&room)
	userID := uint(1)
	node := models.Node{Name: "Node-Idempotent", Token: "token-idempotent", CurrentUserID: &userID, Status: models.NodeStatusIdle}
	db.Create(&node)
	r := setupNodeApiRouter()

	startInput := map[string]any{
		"action":           "start",
		"room_id":          room.ID,
		"subject":          "Physics",
		"start_time":       time.Now(),
		"duration_minutes": 120,
		"examinee_count":   30,
	}
	body, _ := json.Marshal(startInput)

	// 第一次 start
	req1 := httptest.NewRequest("POST", "/node-api/v1/tasks/sync", bytes.NewBuffer(body))
	req1.Header.Set("X-Node-Token", "token-idempotent")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected first start 200, got %d", w1.Code)
	}

	// 第二次 start（同节点）
	req2 := httptest.NewRequest("POST", "/node-api/v1/tasks/sync", bytes.NewBuffer(body))
	req2.Header.Set("X-Node-Token", "token-idempotent")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected second start 200 for idempotency, got %d", w2.Code)
	}

	var count int64
	if err := db.Model(&models.Exam{}).Where("node_id = ? AND end_time IS NULL", node.ID).Count(&count).Error; err != nil {
		t.Fatalf("count active exams failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 active exam for node, got %d", count)
	}
}

func TestNodeHeartbeat_IdleSelfHealsStaleCurrentExam(t *testing.T) {
	db := setupNodeTestDB(t)

	user := models.User{Username: "u-selfheal", Password: "p", Role: models.Proctor}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	room := models.Room{Name: "Room-selfheal", Building: "A", RTSPUrl: "rtsp://example.com/selfheal"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{Name: "Node-selfheal", Token: "token-selfheal", Status: models.NodeStatusError, CurrentUserID: &user.ID}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	endedAt := time.Now()
	nodeID := node.ID
	exam := models.Exam{
		Name:            "Stale Exam",
		Subject:         "Math",
		RoomID:          room.ID,
		NodeID:          &nodeID,
		UserID:          user.ID,
		DurationSeconds: 3600,
		StartTime:       time.Now().Add(-2 * time.Hour),
		EndTime:         &endedAt,
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}
	if err := db.Model(&models.Node{}).Where("id = ?", node.ID).Update("current_exam_id", exam.ID).Error; err != nil {
		t.Fatalf("set node current_exam_id failed: %v", err)
	}

	r := setupNodeApiRouter()
	input := map[string]string{"status": models.NodeStatusIdle}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/node-api/v1/heartbeat", bytes.NewBuffer(body))
	req.Header.Set("X-Node-Token", "token-selfheal")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var refreshed models.Node
	if err := db.First(&refreshed, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if refreshed.Status != models.NodeStatusIdle {
		t.Fatalf("expected node status idle after self-heal, got %s", refreshed.Status)
	}
	if refreshed.CurrentExamID != nil {
		t.Fatalf("expected current_exam_id cleared after self-heal")
	}
}

func TestNodeHeartbeat_InvalidStatusRejected(t *testing.T) {
	db := setupNodeTestDB(t)
	node := models.Node{Name: "Node-invalid-status", Token: "token-invalid-status", Status: models.NodeStatusIdle}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	r := setupNodeApiRouter()
	input := map[string]string{"status": "unknown_status"}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/node-api/v1/heartbeat", bytes.NewBuffer(body))
	req.Header.Set("X-Node-Token", "token-invalid-status")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d", w.Code)
	}
}
