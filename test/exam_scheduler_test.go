package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cc/models"
	"cc/tasks"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForExamScheduler(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	models.DB = db
	return db
}

func setupFileTestDBForExamScheduler(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "exam_scheduler_concurrency.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect file test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	models.DB = db
	return db
}

func TestRetryScheduleExam_AssignAndNotifySuccess(t *testing.T) {
	db := setupTestDBForExamScheduler(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exam/start" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
			return
		}
		if r.URL.Query().Get("token") != "token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success": false, "error": "unauthorized"}`))
			return
		}
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success": false, "error": "invalid json"}`))
			return
		}
		if len(payload) != 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success": false, "error": "payload fields mismatch"}`))
			return
		}
		if payload["subject"] != "Math" || payload["duration"] != "60" || payload["classroom_id"] == nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success": false, "error": "missing required fields"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "proctor", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R101", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "node-1",
		Token:           "token-1",
		Model:           "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	exam := models.Exam{
		Name:            "Math考试",
		Subject:         "Math",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	if err := tasks.RetryScheduleExam(exam.ID); err != nil {
		t.Fatalf("retry schedule failed: %v", err)
	}

	var refreshed models.Exam
	if err := db.First(&refreshed, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshed.NodeID == nil {
		t.Fatalf("expected node assigned, got nil")
	}
	if refreshed.ScheduleStatus != models.ExamScheduleRunning {
		t.Fatalf("expected schedule status running, got %s", refreshed.ScheduleStatus)
	}

	var refreshedNode models.Node
	if err := db.First(&refreshedNode, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if refreshedNode.CurrentExamID == nil || *refreshedNode.CurrentExamID != exam.ID {
		t.Fatalf("expected node current_exam_id=%d", exam.ID)
	}
}

func TestRetryScheduleExam_NoAvailableNode(t *testing.T) {
	db := setupTestDBForExamScheduler(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "proctor", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R101", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	exam := models.Exam{
		Name:            "Math考试",
		Subject:         "Math",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	if err := tasks.RetryScheduleExam(exam.ID); err == nil {
		t.Fatalf("expected retry schedule to fail when no available nodes")
	}

	var refreshed models.Exam
	if err := db.First(&refreshed, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshed.ScheduleStatus != models.ExamScheduleAssignFail {
		t.Fatalf("expected schedule status assign_failed, got %s", refreshed.ScheduleStatus)
	}
}

func TestRetryScheduleExam_RunningDoesNotNotifyAgain(t *testing.T) {
	db := setupTestDBForExamScheduler(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCount := 0
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exam/start" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
			return
		}
		notifyCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "proctor", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R101", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "node-1",
		Token:           "token-1",
		Model:           "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusBusy,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	nodeID := node.ID
	exam := models.Exam{
		Name:            "Math考试",
		Subject:         "Math",
		RoomID:          room.ID,
		NodeID:          &nodeID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}
	if err := db.Model(&models.Node{}).Where("id = ?", node.ID).Update("current_exam_id", exam.ID).Error; err != nil {
		t.Fatalf("set node current_exam_id failed: %v", err)
	}

	if err := tasks.RetryScheduleExam(exam.ID); err != nil {
		t.Fatalf("retry schedule failed: %v", err)
	}

	if notifyCount != 0 {
		t.Fatalf("expected no duplicate notify for running exam, got %d", notifyCount)
	}
}

func TestRetryScheduleExam_ConcurrentRequestsOneNode(t *testing.T) {
	db := setupFileTestDBForExamScheduler(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exam/start" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "proctor", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R-Concurrent", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "node-concurrent",
		Token:           "token-concurrent",
		Model:           "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	examA := models.Exam{
		Name:            "Exam-A",
		Subject:         "Math",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&examA).Error; err != nil {
		t.Fatalf("create examA failed: %v", err)
	}

	examB := models.Exam{
		Name:            "Exam-B",
		Subject:         "English",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&examB).Error; err != nil {
		t.Fatalf("create examB failed: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errCh <- tasks.RetryScheduleExam(examA.ID)
	}()
	go func() {
		defer wg.Done()
		errCh <- tasks.RetryScheduleExam(examB.ID)
	}()
	wg.Wait()
	close(errCh)

	var runningCount int64
	if err := db.Model(&models.Exam{}).Where("schedule_status = ?", models.ExamScheduleRunning).Count(&runningCount).Error; err != nil {
		t.Fatalf("count running exams failed: %v", err)
	}
	if runningCount != 1 {
		t.Fatalf("expected exactly one running exam after concurrent retries, got %d", runningCount)
	}

	var assignFailCount int64
	if err := db.Model(&models.Exam{}).Where("schedule_status = ?", models.ExamScheduleAssignFail).Count(&assignFailCount).Error; err != nil {
		t.Fatalf("count assign_failed exams failed: %v", err)
	}
	if assignFailCount != 1 {
		t.Fatalf("expected exactly one assign_failed exam after concurrent retries, got %d", assignFailCount)
	}
}
