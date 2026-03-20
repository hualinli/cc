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

func setupTestDBForExamHandlers(t *testing.T) *gorm.DB {
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

	models.DB = db
	return db
}

func setupExamRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("mysession", store))

	r.GET("/exams", handlers.ListExams)
	r.GET("/exams/:id", handlers.GetExams)
	r.POST("/exams", handlers.CreateExam)
	r.DELETE("/exams/:id", handlers.DeleteExam)
	r.PUT("/exams/:id", handlers.UpdateExam)
	r.GET("/exams/stats", handlers.GetExamStats)
	r.POST("/exams/:id/retry-schedule", handlers.RetryAssignAndNotifyExam)

	return r
}

func TestCreateExamWithoutNode(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	router := setupExamRouter()

	examData := map[string]interface{}{
		"name":             "Test Exam No Node",
		"subject":          "Math",
		"room_id":          1,
		"user_id":          1,
		"start_time":       time.Now(),
		"duration_seconds": 3600,
		"examinee_count":   40,
	}
	jsonData, _ := json.Marshal(examData)

	req, _ := http.NewRequest("POST", "/exams", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var created models.Exam
	if err := db.Where("name = ?", "Test Exam No Node").First(&created).Error; err != nil {
		t.Fatalf("expected created exam: %v", err)
	}
	if created.NodeID != nil {
		t.Errorf("expected node_id to be nil")
	}
	if created.ScheduleStatus != models.ExamSchedulePending {
		t.Errorf("expected schedule status pending, got %s", created.ScheduleStatus)
	}
}

func TestCreateExam(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	router := setupExamRouter()

	examData := map[string]interface{}{
		"name":             "Test Exam",
		"subject":          "Math",
		"room_id":          1,
		"node_id":          1,
		"user_id":          1,
		"start_time":       time.Now(),
		"duration_seconds": 7200,
		"end_time":         nil,
		"examinee_count":   50,
	}
	jsonData, _ := json.Marshal(examData)

	req, _ := http.NewRequest("POST", "/exams", bytes.NewBuffer(jsonData))
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

func TestListExams(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test exam
	nodeID := uint(1)
	exam := models.Exam{
		Name:            "Test Exam",
		Subject:         "Math",
		RoomID:          1,
		NodeID:          &nodeID,
		UserID:          1,
		DurationSeconds: 7200,
		StartTime:       time.Now(),
		ExamineeCount:   50,
	}
	db.Create(&exam)

	router := setupExamRouter()

	req, _ := http.NewRequest("GET", "/exams", nil)
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
		t.Errorf("Expected 1 exam, got %d", len(data))
	}

	examObj, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected exam object, got %T", data[0])
	}
	userObj, ok := examObj["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected user relation in exam list response, got %T", examObj["user"])
	}
	if username, ok := userObj["username"].(string); !ok || username != "testuser" {
		t.Fatalf("Expected user.username=testuser, got %v", userObj["username"])
	}
}

func TestGetExams(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test exam
	nodeID := uint(1)
	exam := models.Exam{
		Name:            "Test Exam",
		Subject:         "Math",
		RoomID:          1,
		NodeID:          &nodeID,
		UserID:          1,
		DurationSeconds: 7200,
		StartTime:       time.Now(),
		ExamineeCount:   50,
	}
	db.Create(&exam)

	router := setupExamRouter()

	req, _ := http.NewRequest("GET", "/exams/1", nil)
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

func TestUpdateExam(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test exam
	nodeID := uint(1)
	exam := models.Exam{
		Name:            "Test Exam",
		Subject:         "Math",
		RoomID:          1,
		NodeID:          &nodeID,
		UserID:          1,
		DurationSeconds: 7200,
		StartTime:       time.Now(),
		ExamineeCount:   50,
	}
	db.Create(&exam)

	router := setupExamRouter()

	updateData := map[string]interface{}{
		"name": "Updated Exam",
	}
	jsonData, _ := json.Marshal(updateData)

	req, _ := http.NewRequest("PUT", "/exams/1", bytes.NewBuffer(jsonData))
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

func TestDeleteExam(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test exam
	nodeID := uint(1)
	exam := models.Exam{
		Name:            "Test Exam",
		Subject:         "Math",
		RoomID:          1,
		NodeID:          &nodeID,
		UserID:          1,
		DurationSeconds: 7200,
		StartTime:       time.Now(),
		ExamineeCount:   50,
	}
	db.Create(&exam)

	router := setupExamRouter()

	req, _ := http.NewRequest("DELETE", "/exams/1", nil)
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

func TestGetExamStats(t *testing.T) {
	db := setupTestDBForExamHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create a test exam first to get its ID
	nodeID := uint(2)
	exam := models.Exam{
		Name:            "Test Exam",
		Subject:         "Math",
		RoomID:          1,
		NodeID:          &nodeID,
		UserID:          1,
		DurationSeconds: 7200,
		StartTime:       time.Now(),
		ExamineeCount:   50,
	}
	db.Create(&exam)

	// Create a busy node with this exam
	node := models.Node{
		Name:          "Busy Node",
		Token:         "token2",
		Model:         "model",
		Address:       "address",
		Status:        models.NodeStatusBusy,
		Version:       "1.0.0",
		CurrentExamID: &exam.ID,
	}
	db.Create(&node)

	router := setupExamRouter()

	req, _ := http.NewRequest("GET", "/exams/stats", nil)
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
