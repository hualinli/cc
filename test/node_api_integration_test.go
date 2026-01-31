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
