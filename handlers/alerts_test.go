package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cc/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAlertsHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&models.Room{}, &models.User{}, &models.Exam{}, &models.Alert{}); err != nil {
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

func seedAlertRoom(t *testing.T) models.Room {
	t.Helper()
	room := models.Room{Name: "R101", Building: "Main", RTSPUrl: "rtsp://camera"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedAlertUser(t *testing.T) models.User {
	t.Helper()
	user := models.User{Username: "alert-user", Password: "does-not-matter", Role: models.Proctor}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedAlertExam(t *testing.T, roomID, userID uint) models.Exam {
	t.Helper()
	exam := models.Exam{Name: "exam-1", Subject: "math", RoomID: roomID, UserID: userID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}
	return exam
}

func setupAlertsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/alerts", CreateAlert)
	r.GET("/alerts", ListAlerts)
	r.GET("/alerts/:id", GetAlerts)
	r.PUT("/alerts/:id", UpdateAlert)
	r.DELETE("/alerts/:id", DeleteAlert)
	return r
}

func decodeResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestCreateAlert_Success(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)

	r := setupAlertsRouter()
	body := `{"exam_id":` + fmt.Sprint(exam.ID) + `,"type":"phone_cheating","seat_number":"A1","message":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeResp(t, w)
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp["success"])
	}
	data := resp["data"].(map[string]any)
	if data["exam_id"] != float64(exam.ID) {
		t.Fatalf("expected exam_id %d, got %v", exam.ID, data["exam_id"])
	}
	if data["type"] != "phone_cheating" {
		t.Fatalf("expected type phone_cheating, got %v", data["type"])
	}
	if data["exam"] == nil {
		t.Fatal("expected exam to be preloaded")
	}
}

func TestCreateAlert_InvalidExamID(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	r := setupAlertsRouter()
	body := `{"exam_id":999,"type":"phone_cheating"}`
	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	resp := decodeResp(t, w)
	if resp["error"] != "exam_id 无效" {
		t.Fatalf("expected error exam_id 无效, got %v", resp["error"])
	}
}

func TestCreateAlert_InvalidType(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)

	r := setupAlertsRouter()
	body := `{"exam_id":` + fmt.Sprint(exam.ID) + `,"type":"invalid_type"}`
	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp["error"] != "type 无效" {
		t.Fatalf("expected error type 无效, got %v", resp["error"])
	}
}

func TestGetAlerts_Success(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)
	alert := models.Alert{ExamID: exam.ID, Type: models.AlertTypePhoneCheating, SeatNumber: "B2", Message: "hello", CreatedAt: time.Now()}
	if err := models.DB.Create(&alert).Error; err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	r := setupAlertsRouter()
	req := httptest.NewRequest(http.MethodGet, "/alerts/"+fmt.Sprint(alert.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp["success"])
	}
}

func TestGetAlerts_NotFound(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	r := setupAlertsRouter()
	req := httptest.NewRequest(http.MethodGet, "/alerts/12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestUpdateAlert_Success(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam1 := seedAlertExam(t, room.ID, user.ID)
	exam2 := seedAlertExam(t, room.ID, user.ID)
	alert := models.Alert{ExamID: exam1.ID, Type: models.AlertTypeLookAround, SeatNumber: "C3", Message: "before", CreatedAt: time.Now()}
	if err := models.DB.Create(&alert).Error; err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	r := setupAlertsRouter()
	body := `{"message":"after","exam_id":` + fmt.Sprint(exam2.ID) + `}`
	req := httptest.NewRequest(http.MethodPut, "/alerts/"+fmt.Sprint(alert.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	data := resp["data"].(map[string]any)
	if data["message"] != "after" {
		t.Fatalf("expected message after, got %v", data["message"])
	}
	if data["exam_id"] != float64(exam2.ID) {
		t.Fatalf("expected exam_id %d, got %v", exam2.ID, data["exam_id"])
	}
}

func TestUpdateAlert_InvalidExamID(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)
	alert := models.Alert{ExamID: exam.ID, Type: models.AlertTypeOther, SeatNumber: "D4", CreatedAt: time.Now()}
	if err := models.DB.Create(&alert).Error; err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	r := setupAlertsRouter()
	body := `{"exam_id":9999}`
	req := httptest.NewRequest(http.MethodPut, "/alerts/"+fmt.Sprint(alert.ID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	if resp["error"] != "exam_id 无效" {
		t.Fatalf("expected exam_id 无效, got %v", resp["error"])
	}
}

func TestDeleteAlert_Success(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)
	alert := models.Alert{ExamID: exam.ID, Type: models.AlertTypeStandUp, CreatedAt: time.Now()}
	if err := models.DB.Create(&alert).Error; err != nil {
		t.Fatalf("failed to create alert: %v", err)
	}

	r := setupAlertsRouter()
	req := httptest.NewRequest(http.MethodDelete, "/alerts/"+fmt.Sprint(alert.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDeleteAlert_NotFound(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	r := setupAlertsRouter()
	req := httptest.NewRequest(http.MethodDelete, "/alerts/9999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListAlerts_FilterByType(t *testing.T) {
	cleanup := setupAlertsHandlerTestDB(t)
	defer cleanup()

	room := seedAlertRoom(t)
	user := seedAlertUser(t)
	exam := seedAlertExam(t, room.ID, user.ID)
	alerts := []models.Alert{
		{ExamID: exam.ID, Type: models.AlertTypePhoneCheating, SeatNumber: "A1", CreatedAt: time.Now()},
		{ExamID: exam.ID, Type: models.AlertTypeWhispering, SeatNumber: "A2", CreatedAt: time.Now()},
	}
	if err := models.DB.Create(&alerts).Error; err != nil {
		t.Fatalf("failed to create alerts: %v", err)
	}

	r := setupAlertsRouter()
	req := httptest.NewRequest(http.MethodGet, "/alerts?type=phone_cheating", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResp(t, w)
	data := resp["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(data))
	}
}
