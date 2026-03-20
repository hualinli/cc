package tasks

import (
	"cc/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupExamSchedulerProcessTestDB(t *testing.T) *gorm.DB {
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

func TestProcessDueExams_OnlySchedulesOneExamPerTick(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCount := 0
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/start" && r.Method == http.MethodPost {
			notifyCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
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

	baseNode := models.Node{
		Token:           "token-1",
		Model:           "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}

	node1 := baseNode
	node1.Name = "node-1"
	if err := db.Create(&node1).Error; err != nil {
		t.Fatalf("create node1 failed: %v", err)
	}

	node2 := baseNode
	node2.Name = "node-2"
	node2.Token = "token-2"
	if err := db.Create(&node2).Error; err != nil {
		t.Fatalf("create node2 failed: %v", err)
	}

	exam1 := models.Exam{
		Name:            "Exam-1",
		Subject:         "Math",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-2 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam1).Error; err != nil {
		t.Fatalf("create exam1 failed: %v", err)
	}

	exam2 := models.Exam{
		Name:            "Exam-2",
		Subject:         "English",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam2).Error; err != nil {
		t.Fatalf("create exam2 failed: %v", err)
	}

	processDueExams()

	if notifyCount != 1 {
		t.Fatalf("expected exactly 1 node notification in one tick, got %d", notifyCount)
	}

	var runningCount int64
	if err := db.Model(&models.Exam{}).Where("schedule_status = ?", models.ExamScheduleRunning).Count(&runningCount).Error; err != nil {
		t.Fatalf("count running exams failed: %v", err)
	}
	if runningCount != 1 {
		t.Fatalf("expected exactly 1 running exam after one tick, got %d", runningCount)
	}

	var busyCount int64
	if err := db.Model(&models.Node{}).Where("status = ?", models.NodeStatusBusy).Count(&busyCount).Error; err != nil {
		t.Fatalf("count busy nodes failed: %v", err)
	}
	if busyCount != 1 {
		t.Fatalf("expected exactly 1 busy node after one tick, got %d", busyCount)
	}
}

func TestProcessDueExams_SameStartTimeSchedulesInStableOrder(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCount := 0
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/start" && r.Method == http.MethodPost {
			notifyCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "proctor", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R201", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	baseNode := models.Node{
		Token:           "token-a",
		Model:           "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}

	node1 := baseNode
	node1.Name = "node-a"
	if err := db.Create(&node1).Error; err != nil {
		t.Fatalf("create node1 failed: %v", err)
	}
	node2 := baseNode
	node2.Name = "node-b"
	node2.Token = "token-b"
	if err := db.Create(&node2).Error; err != nil {
		t.Fatalf("create node2 failed: %v", err)
	}

	sameStart := time.Now().Add(-2 * time.Minute)
	examA := models.Exam{
		Name:            "Exam-A",
		Subject:         "Math",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       sameStart,
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
		StartTime:       sameStart,
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&examB).Error; err != nil {
		t.Fatalf("create examB failed: %v", err)
	}

	if !(examA.ID < examB.ID) {
		t.Fatalf("expected examA to have smaller ID than examB")
	}

	processDueExams()

	if notifyCount != 1 {
		t.Fatalf("expected 1 notification after first tick, got %d", notifyCount)
	}

	var firstA, firstB models.Exam
	if err := db.First(&firstA, examA.ID).Error; err != nil {
		t.Fatalf("reload examA failed: %v", err)
	}
	if err := db.First(&firstB, examB.ID).Error; err != nil {
		t.Fatalf("reload examB failed: %v", err)
	}
	if firstA.ScheduleStatus != models.ExamScheduleRunning {
		t.Fatalf("expected examA running after first tick, got %s", firstA.ScheduleStatus)
	}
	if firstB.ScheduleStatus != models.ExamSchedulePending {
		t.Fatalf("expected examB still pending after first tick, got %s", firstB.ScheduleStatus)
	}

	processDueExams()

	if notifyCount != 2 {
		t.Fatalf("expected 2 notifications after second tick, got %d", notifyCount)
	}

	var secondB models.Exam
	if err := db.First(&secondB, examB.ID).Error; err != nil {
		t.Fatalf("reload examB failed: %v", err)
	}
	if secondB.ScheduleStatus != models.ExamScheduleRunning {
		t.Fatalf("expected examB running after second tick, got %s", secondB.ScheduleStatus)
	}
}
