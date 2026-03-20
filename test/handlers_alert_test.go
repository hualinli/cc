package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"cc/handlers"
	"cc/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForAlertHandlers(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create test data
	user := models.User{Username: "testuser", Password: "password", Role: "admin"}
	db.Create(&user)

	room := models.Room{Name: "Test Room", Building: "A", RTSPUrl: "rtsp://example.com/stream"}
	db.Create(&room)

	node := models.Node{
		Name:    "Test Node",
		Token:   "token",
		Model:   "model",
		Address: "address",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)
	nodeID := node.ID

	exam := models.Exam{
		Name:          "Test Exam",
		Subject:       "Math",
		RoomID:        room.ID,
		NodeID:        &nodeID,
		UserID:        user.ID,
		StartTime:     time.Now(),
		ExamineeCount: 50,
	}
	db.Create(&exam)

	models.DB = db
	return db
}

func setupAlertRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("mysession", store))

	r.GET("/alerts", handlers.ListAlerts)
	r.GET("/alerts/:id", handlers.GetAlerts)
	r.POST("/alerts", handlers.CreateAlert)
	r.DELETE("/alerts/:id", handlers.DeleteAlert)
	r.PUT("/alerts/:id", handlers.UpdateAlert)

	return r
}

func TestCreateAlert(t *testing.T) {
	db := setupTestDBForAlertHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	router := setupAlertRouter()

	alertData := map[string]interface{}{
		"exam_id":      1,
		"type":         "phone_cheating",
		"seat_number":  "A01",
		"x":            100.0,
		"y":            200.0,
		"message":      "Test alert",
		"picture_path": "/uploads/test.jpg",
	}
	jsonData, _ := json.Marshal(alertData)

	req, _ := http.NewRequest("POST", "/alerts", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if !response["success"].(bool) {
		t.Errorf("Expected success true, got %v", response["success"])
	}
}

func TestListAlerts(t *testing.T) {
	db := setupTestDBForAlertHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test alert
	alert := models.Alert{
		ExamID:      1,
		Type:        models.AlertTypePhoneCheating,
		SeatNumber:  "A01",
		X:           100.0,
		Y:           200.0,
		Message:     "Test alert",
		PicturePath: "/uploads/test.jpg",
		CreatedAt:   time.Now(),
	}
	db.Create(&alert)

	router := setupAlertRouter()

	req, _ := http.NewRequest("GET", "/alerts", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if success, ok := response["success"].(bool); !ok || !success {
		t.Errorf("Expected success true, got %v (error: %v)", response["success"], response["error"])
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatalf("Expected data to be a slice, got %T", response["data"])
	}
	if len(data) != 1 {
		t.Errorf("Expected 1 alert, got %d", len(data))
	}
}

func TestGetAlerts(t *testing.T) {
	db := setupTestDBForAlertHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test alert
	alert := models.Alert{
		ExamID:      1,
		Type:        models.AlertTypePhoneCheating,
		SeatNumber:  "A01",
		X:           100.0,
		Y:           200.0,
		Message:     "Test alert",
		PicturePath: "/uploads/test.jpg",
		CreatedAt:   time.Now(),
	}
	db.Create(&alert)

	router := setupAlertRouter()

	req, _ := http.NewRequest("GET", "/alerts/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if !response["success"].(bool) {
		t.Errorf("Expected success true, got %v", response["success"])
	}
}

func TestUpdateAlert(t *testing.T) {
	db := setupTestDBForAlertHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test alert
	alert := models.Alert{
		ExamID:      1,
		Type:        models.AlertTypePhoneCheating,
		SeatNumber:  "A01",
		X:           100.0,
		Y:           200.0,
		Message:     "Test alert",
		PicturePath: "/uploads/test.jpg",
		CreatedAt:   time.Now(),
	}
	db.Create(&alert)

	router := setupAlertRouter()

	updateData := map[string]interface{}{
		"message": "Updated alert",
	}
	jsonData, _ := json.Marshal(updateData)

	req, _ := http.NewRequest("PUT", "/alerts/1", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if !response["success"].(bool) {
		t.Errorf("Expected success true, got %v", response["success"])
	}
}

func TestDeleteAlert(t *testing.T) {
	db := setupTestDBForAlertHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test alert
	alert := models.Alert{
		ExamID:      1,
		Type:        models.AlertTypePhoneCheating,
		SeatNumber:  "A01",
		X:           100.0,
		Y:           200.0,
		Message:     "Test alert",
		PicturePath: "/uploads/test.jpg",
		CreatedAt:   time.Now(),
	}
	db.Create(&alert)

	router := setupAlertRouter()

	req, _ := http.NewRequest("DELETE", "/alerts/1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if !response["success"].(bool) {
		t.Errorf("Expected success true, got %v", response["success"])
	}
}
