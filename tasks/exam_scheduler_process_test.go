package tasks

import (
	"cc/models"
	"encoding/json"
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
		NodeModel:       "m1",
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
		NodeModel:       "m1",
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

func TestProcessDueExams_SkipsDirtyIdleNodeWithCurrentExamID(t *testing.T) {
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

	user := models.User{Username: "scheduler-user", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R301", Building: "A", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	dirtyExamID := uint(999)
	dirtyNode := models.Node{
		Name:            "dirty-idle-node",
		Token:           "dirty-token",
		NodeModel:       "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		CurrentExamID:   &dirtyExamID,
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&dirtyNode).Error; err != nil {
		t.Fatalf("create dirty node failed: %v", err)
	}

	cleanNode := models.Node{
		Name:            "clean-idle-node",
		Token:           "clean-token",
		NodeModel:       "m1",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&cleanNode).Error; err != nil {
		t.Fatalf("create clean node failed: %v", err)
	}

	exam := models.Exam{
		Name:            "Exam-Dirty-Node-Bypass",
		Subject:         "Physics",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	processDueExams()

	if notifyCount != 1 {
		t.Fatalf("expected 1 notification, got %d", notifyCount)
	}

	var reloadedExam models.Exam
	if err := db.First(&reloadedExam, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if reloadedExam.NodeID == nil {
		t.Fatalf("expected exam assigned to a node")
	}
	if *reloadedExam.NodeID != cleanNode.ID {
		t.Fatalf("expected exam assigned to clean node=%d, got %d", cleanNode.ID, *reloadedExam.NodeID)
	}
}

func TestScheduleExamByID_DoesNotSetRunningIfExamEndedDuringNotify(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	var examID uint
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/start" && r.Method == http.MethodPost {
			now := time.Now()
			if err := db.Model(&models.Exam{}).Where("id = ?", examID).Updates(map[string]any{
				"end_time":   now,
				"updated_at": now,
			}).Error; err != nil {
				t.Fatalf("end exam in notify handler failed: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "notify-race-user", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R401", Building: "B", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "notify-race-node",
		Token:           "notify-race-token",
		NodeModel:       "m2",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	exam := models.Exam{
		Name:            "Exam-Notify-Race",
		Subject:         "Chemistry",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 1800,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}
	examID = exam.ID

	if err := scheduleExamByID(exam.ID, false); err != nil {
		t.Fatalf("schedule exam failed: %v", err)
	}

	var reloadedExam models.Exam
	if err := db.First(&reloadedExam, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if reloadedExam.EndTime == nil {
		t.Fatalf("expected exam ended by concurrent notify-side update")
	}
	if reloadedExam.ScheduleStatus == models.ExamScheduleRunning {
		t.Fatalf("expected exam not forced to running after it already ended")
	}

	var reloadedNode models.Node
	if err := db.First(&reloadedNode, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if reloadedNode.CurrentExamID != nil && *reloadedNode.CurrentExamID == exam.ID {
		encoded, _ := json.Marshal(reloadedNode)
		t.Fatalf("expected node lock to be released by end-exam flow, got node=%s", string(encoded))
	}
}
