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

func setupExamsHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		models.DB = oldDB
		_ = sqlDB.Close()
	}
}

func seedExamRoom(t *testing.T) models.Room {
	t.Helper()
	room := models.Room{Name: "R101", Building: "Main", RTSPUrl: "rtsp://camera"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedExamUser(t *testing.T) models.User {
	t.Helper()
	user := models.User{Username: "exam-user", Password: "pass", Role: models.Proctor}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedExamNode(t *testing.T) models.Node {
	t.Helper()
	node := models.Node{Name: "node-1", Token: "token-1", Status: models.NodeStatusIdle}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

func setupExamsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/exams", CreateExam)
	r.DELETE("/exams/:id", DeleteExam)
	r.PUT("/exams/:id", UpdateExam)
	r.GET("/exams/:id", GetExams)
	r.GET("/exams", ListExams)
	r.GET("/exams/stats", GetExamStats)
	return r
}

func decodeExamResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func performExamJSONRequest(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateExam_SuccessAssignedNode(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	node := seedExamNode(t)

	r := setupExamsRouter()
	body := fmt.Sprintf(
		`{"subject":"math","room_id":%d,"user_id":%d,"node_id":%d,"start_time":"%s","duration_minutes":30,"examinee_count":20}`,
		room.ID,
		user.ID,
		node.ID,
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	)

	w := performExamJSONRequest(t, r, http.MethodPost, "/exams", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	resp := decodeExamResp(t, w)
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp["success"])
	}
	data := resp["data"].(map[string]any)
	if data["schedule_status"] != models.ExamScheduleAssigned {
		t.Fatalf("expected assigned status, got %v", data["schedule_status"])
	}
	if data["name"] != "math考试" {
		t.Fatalf("expected default name math考试, got %v", data["name"])
	}

	var refreshedNode models.Node
	if err := models.DB.First(&refreshedNode, node.ID).Error; err != nil {
		t.Fatalf("failed to load node: %v", err)
	}
	if refreshedNode.CurrentExamID == nil {
		t.Fatal("expected node current_exam_id to be set")
	}
	if refreshedNode.Status != models.NodeStatusBusy {
		t.Fatalf("expected node busy, got %s", refreshedNode.Status)
	}
}

func TestCreateExam_InvalidEndTime(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)

	r := setupExamsRouter()
	startTime := time.Now().Add(time.Hour).UTC()
	endTime := startTime.Add(-time.Minute)
	body := fmt.Sprintf(
		`{"subject":"math","name":"test","room_id":%d,"user_id":%d,"start_time":"%s","end_time":"%s","duration_seconds":3600}`,
		room.ID,
		user.ID,
		startTime.Format(time.RFC3339),
		endTime.Format(time.RFC3339),
	)

	w := performExamJSONRequest(t, r, http.MethodPost, "/exams", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	if resp["error"] != "end_time 必须不早于 start_time" {
		t.Fatalf("expected end_time invalid error, got %v", resp["error"])
	}
}

func TestDeleteExam_Success(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "to-delete", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), EndTime: ptrTime(time.Now()), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodDelete, "/exams/"+fmt.Sprint(exam.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var count int64
	models.DB.Model(&models.Exam{}).Where("id = ?", exam.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected exam deleted, still found %d", count)
	}
}

func TestDeleteExam_PendingUnscheduledAllowed(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "pending-delete", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodDelete, "/exams/"+fmt.Sprint(exam.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var count int64
	models.DB.Model(&models.Exam{}).Where("id = ?", exam.ID).Count(&count)
	if count != 0 {
		t.Fatalf("expected pending exam deleted, still found %d", count)
	}
}

func TestDeleteExam_DeletesRelatedAlerts(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "alert-cascade-delete", Subject: "english", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}
	alert1 := models.Alert{ExamID: exam.ID, Type: models.AlertTypePhoneCheating, SeatNumber: "A1", Message: "issue1"}
	alert2 := models.Alert{ExamID: exam.ID, Type: models.AlertTypeLookAround, SeatNumber: "A2", Message: "issue2"}
	if err := models.DB.Create(&alert1).Error; err != nil {
		t.Fatalf("failed to seed alert1: %v", err)
	}
	if err := models.DB.Create(&alert2).Error; err != nil {
		t.Fatalf("failed to seed alert2: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodDelete, "/exams/"+fmt.Sprint(exam.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var alertCount int64
	models.DB.Model(&models.Alert{}).Where("exam_id = ?", exam.ID).Count(&alertCount)
	if alertCount != 0 {
		t.Fatalf("expected alerts deleted with exam, still found %d", alertCount)
	}
}

func TestDeleteExam_ActiveExamConflict(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "running", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamScheduleRunning}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodDelete, "/exams/"+fmt.Sprint(exam.ID), "")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestUpdateExam_InvalidRoomID(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "update-test", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	body := `{"room_id":9999}`
	w := performExamJSONRequest(t, r, http.MethodPut, "/exams/"+fmt.Sprint(exam.ID), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	if resp["error"] != "room_id 无效" {
		t.Fatalf("expected room_id invalid error, got %v", resp["error"])
	}
}

func TestUpdateExam_Success(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "update-test", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	body := `{"name":"updated exam","subject":"physics","examinee_count":35}`
	w := performExamJSONRequest(t, r, http.MethodPut, "/exams/"+fmt.Sprint(exam.ID), body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	data := resp["data"].(map[string]any)
	if data["name"] != "updated exam" {
		t.Fatalf("expected updated name, got %v", data["name"])
	}
	if data["subject"] != "physics" {
		t.Fatalf("expected updated subject, got %v", data["subject"])
	}
	if data["examinee_count"] != float64(35) {
		t.Fatalf("expected examinee_count 35, got %v", data["examinee_count"])
	}
}

func TestUpdateExam_UpdateRemark(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "update-test", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	body := `{"remark":"考试现场遇到临时问题"}`
	w := performExamJSONRequest(t, r, http.MethodPut, "/exams/"+fmt.Sprint(exam.ID), body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	data := resp["data"].(map[string]any)
	if data["remark"] != "考试现场遇到临时问题" {
		t.Fatalf("expected remark updated, got %v", data["remark"])
	}
}

func TestUpdateExam_InvalidSubjectEmpty(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "update-test", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	r := setupExamsRouter()
	body := `{"subject":"   "}`
	w := performExamJSONRequest(t, r, http.MethodPut, "/exams/"+fmt.Sprint(exam.ID), body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	if resp["error"] != "subject 不能为空" {
		t.Fatalf("expected subject empty error, got %v", resp["error"])
	}
}

func TestGetExams_Success(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam := models.Exam{Name: "details", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamScheduleRunning}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}
	alert := models.Alert{ExamID: exam.ID, Type: models.AlertTypePhoneCheating, SeatNumber: "A1", Message: "issue"}
	if err := models.DB.Create(&alert).Error; err != nil {
		t.Fatalf("failed to seed alert: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodGet, "/exams/"+fmt.Sprint(exam.ID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeExamResp(t, w)
	data := resp["data"].(map[string]any)
	if data["anomalies_count"] != float64(1) {
		t.Fatalf("expected anomalies_count 1, got %v", data["anomalies_count"])
	}
}

func TestListExams_InvalidRoomID(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodGet, "/exams?room_id=abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListExams_InvalidDate(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodGet, "/exams?date=2023-02-30", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetExamStats_Empty(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodGet, "/exams/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	data := resp["data"].(map[string]any)
	if data["total_rooms"] != float64(0) {
		t.Fatalf("expected total_rooms 0, got %v", data["total_rooms"])
	}
	if data["total_students"] != float64(0) {
		t.Fatalf("expected total_students 0, got %v", data["total_students"])
	}
	if data["total_anomalies"] != float64(0) {
		t.Fatalf("expected total_anomalies 0, got %v", data["total_anomalies"])
	}
	if data["anomaly_coeff"] != float64(0) {
		t.Fatalf("expected anomaly_coeff 0, got %v", data["anomaly_coeff"])
	}
	ongoing := data["ongoing_exams"].([]any)
	if len(ongoing) != 0 {
		t.Fatalf("expected no ongoing exams, got %d", len(ongoing))
	}
}

func TestGetExamStats_WithRunningExams(t *testing.T) {
	cleanup := setupExamsHandlerTestDB(t)
	defer cleanup()

	room := seedExamRoom(t)
	user := seedExamUser(t)
	exam1 := models.Exam{Name: "running1", Subject: "math", RoomID: room.ID, UserID: user.ID, StartTime: time.Now(), ScheduleStatus: models.ExamScheduleRunning, ExamineeCount: 10}
	exam2 := models.Exam{Name: "running2", Subject: "physics", RoomID: room.ID, UserID: user.ID, StartTime: time.Now().Add(time.Minute), ScheduleStatus: models.ExamScheduleRunning, ExamineeCount: 20}
	exam3 := models.Exam{Name: "pending", Subject: "history", RoomID: room.ID, UserID: user.ID, StartTime: time.Now().Add(2 * time.Minute), ScheduleStatus: models.ExamSchedulePending}
	if err := models.DB.Create(&exam1).Error; err != nil {
		t.Fatalf("failed to seed exam1: %v", err)
	}
	if err := models.DB.Create(&exam2).Error; err != nil {
		t.Fatalf("failed to seed exam2: %v", err)
	}
	if err := models.DB.Create(&exam3).Error; err != nil {
		t.Fatalf("failed to seed exam3: %v", err)
	}
	alert1 := models.Alert{ExamID: exam1.ID, Type: models.AlertTypePhoneCheating, SeatNumber: "A1", Message: "issue1"}
	alert2 := models.Alert{ExamID: exam1.ID, Type: models.AlertTypeLookAround, SeatNumber: "A2", Message: "issue2"}
	alert3 := models.Alert{ExamID: exam2.ID, Type: models.AlertTypeOther, SeatNumber: "B1", Message: "issue3"}
	if err := models.DB.Create(&alert1).Error; err != nil {
		t.Fatalf("failed to seed alert1: %v", err)
	}
	if err := models.DB.Create(&alert2).Error; err != nil {
		t.Fatalf("failed to seed alert2: %v", err)
	}
	if err := models.DB.Create(&alert3).Error; err != nil {
		t.Fatalf("failed to seed alert3: %v", err)
	}

	r := setupExamsRouter()
	w := performExamJSONRequest(t, r, http.MethodGet, "/exams/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeExamResp(t, w)
	data := resp["data"].(map[string]any)
	if data["total_rooms"] != float64(2) {
		t.Fatalf("expected total_rooms 2, got %v", data["total_rooms"])
	}
	expectedStudents := exam1.ExamineeCount + exam2.ExamineeCount
	if data["total_students"] != float64(expectedStudents) {
		t.Fatalf("expected total_students %d, got %v", expectedStudents, data["total_students"])
	}
	if data["total_anomalies"] != float64(3) {
		t.Fatalf("expected total_anomalies 3, got %v", data["total_anomalies"])
	}
	expectedCoeff := float64(3) / float64(expectedStudents)
	actualCoeff := data["anomaly_coeff"].(float64)
	diff := actualCoeff - expectedCoeff
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Fatalf("expected anomaly_coeff %.6f, got %.6f", expectedCoeff, actualCoeff)
	}
	ongoing := data["ongoing_exams"].([]any)
	if len(ongoing) != 2 {
		t.Fatalf("expected 2 ongoing exams, got %d", len(ongoing))
	}
	counts := map[string]int64{}
	for _, item := range ongoing {
		examMap := item.(map[string]any)
		counts[examMap["name"].(string)] = int64(examMap["anomalies_count"].(float64))
	}
	if counts["running1"] != 2 {
		t.Fatalf("expected running1 anomalies 2, got %d", counts["running1"])
	}
	if counts["running2"] != 1 {
		t.Fatalf("expected running2 anomalies 1, got %d", counts["running2"])
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
