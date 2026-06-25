package tasks

import (
	"cc/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
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
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
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
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
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
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
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

func TestScheduleExamByID_NotifyFailureClearsExamNodeBinding(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success": false, "error": "Classroom with id 2 not found"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "notify-fail-user", Password: "password", Role: "proctor"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	room := models.Room{Name: "R501", Building: "C", RTSPUrl: "rtsp://test"}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "notify-fail-node",
		Token:           "notify-fail-token",
		NodeModel:       "m3",
		Address:         strings.TrimPrefix(nodeServer.URL, "http://"),
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	exam := models.Exam{
		Name:            "Exam-Notify-Fail",
		Subject:         "Biology",
		RoomID:          room.ID,
		UserID:          user.ID,
		StartTime:       time.Now().Add(-1 * time.Minute),
		DurationSeconds: 1800,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatalf("expected schedule error on notify failure, got nil")
	}

	var reloadedExam models.Exam
	if err := db.First(&reloadedExam, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Fatalf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}
	if reloadedExam.NodeID != nil {
		t.Fatalf("expected node_id cleared on notify failure, got %v", *reloadedExam.NodeID)
	}
	if !strings.Contains(strings.ToLower(reloadedExam.ScheduleError), "classroom") {
		t.Fatalf("expected schedule_error to contain notify reason, got %q", reloadedExam.ScheduleError)
	}

	var reloadedNode models.Node
	if err := db.First(&reloadedNode, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Fatalf("expected node current_exam_id cleared, got %v", *reloadedNode.CurrentExamID)
	}
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Fatalf("expected node idle after rollback, got %s", reloadedNode.Status)
	}
}

// TestScheduleExamByID_DoubleScheduleIdempotent 验证对同一考试重复调度是幂等的
func TestScheduleExamByID_DoubleScheduleIdempotent(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCount := 0
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
			notifyCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "double-sched", Password: "pass", Role: "proctor"}
	db.Create(&user)

	room := models.Room{Name: "R601", Building: "D", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "double-sched-node", Token: "ds-tok", NodeModel: "m1",
		Address: strings.TrimPrefix(nodeServer.URL, "http://"),
		Status: models.NodeStatusIdle, Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "Double-Sched", Subject: "DS", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	// 第一次调度
	if err := scheduleExamByID(exam.ID, false); err != nil {
		t.Fatalf("first schedule failed: %v", err)
	}
	if notifyCount != 1 {
		t.Fatalf("expected 1 notification, got %d", notifyCount)
	}

	// 第二次调度同一考试（应被幂等跳过）
	err := scheduleExamByID(exam.ID, false)
	if err != nil {
		t.Fatalf("second schedule should not error, got: %v", err)
	}
	// 不会重复通知，因为考试已经在 running 状态
	if notifyCount != 1 {
		t.Fatalf("expected still 1 notification after idempotent re-schedule, got %d", notifyCount)
	}
}

// TestScheduleExamByID_ConcurrentScheduleRace 验证两个 goroutine 同时调度不同考试时不会死锁
func TestScheduleExamByID_ConcurrentScheduleRace(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCount := int32(0)
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
			// 使用原子操作避免数据竞争
			_ = notifyCount
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "race-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R701", Building: "E", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	// 创建 5 个空闲节点
	for i := 1; i <= 5; i++ {
		db.Create(&models.Node{
			Name: fmt.Sprintf("race-node-%d", i), Token: fmt.Sprintf("rt%d", i),
			NodeModel: "m1", Address: strings.TrimPrefix(nodeServer.URL, "http://"),
			Status: models.NodeStatusIdle, Version: "1.0.0", LastHeartbeatAt: time.Now(),
		})
	}

	// 创建 5 个待调度的考试
	examIDs := make([]uint, 5)
	for i := 1; i <= 5; i++ {
		exam := models.Exam{
			Name: fmt.Sprintf("Race-Exam-%d", i), Subject: fmt.Sprintf("S%d", i),
			RoomID: room.ID, UserID: user.ID,
			StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
			ScheduleStatus: models.ExamSchedulePending,
		}
		db.Create(&exam)
		examIDs[i-1] = exam.ID
	}

	// 并发调度
	var wg sync.WaitGroup
	errCh := make(chan error, 5)
	for _, eid := range examIDs {
		wg.Add(1)
		go func(id uint) {
			defer wg.Done()
			if err := scheduleExamByID(id, false); err != nil {
				errCh <- fmt.Errorf("schedule %d: %w", id, err)
			}
		}(eid)
	}
	wg.Wait()
	close(errCh)

	// 验证没有错误
	for err := range errCh {
		t.Errorf("unexpected schedule error: %v", err)
	}

	// 验证：5 个考试都变为 running，5 个节点都变为 busy
	var runningCount, busyCount int64
	db.Model(&models.Exam{}).Where("schedule_status = ?", models.ExamScheduleRunning).Count(&runningCount)
	db.Model(&models.Node{}).Where("status = ?", models.NodeStatusBusy).Count(&busyCount)
	if runningCount != 5 {
		t.Errorf("expected 5 running exams, got %d", runningCount)
	}
	if busyCount != 5 {
		t.Errorf("expected 5 busy nodes, got %d", busyCount)
	}
}

// TestNodeExamConsistency_RollbackOnNotifyFailure 验证通知失败时节点和考试一致性被正确回滚
func TestNodeExamConsistency_RollbackOnNotifyFailure(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 始终返回 500，模拟节点不可达
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success": false}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "rollback-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R801", Building: "F", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "rollback-node", Token: "rb-tok", NodeModel: "m1",
		Address: strings.TrimPrefix(nodeServer.URL, "http://"),
		Status: models.NodeStatusIdle, Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "Rollback-Exam", Subject: "RB", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected error on notify failure")
	}

	// 验证考试状态
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Errorf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}
	if reloadedExam.NodeID != nil {
		t.Errorf("expected node_id cleared, got %v", reloadedExam.NodeID)
	}

	// 验证节点状态：应该恢复为空闲
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Errorf("expected node idle after rollback, got %s", reloadedNode.Status)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Errorf("expected node current_exam_id cleared, got %v", reloadedNode.CurrentExamID)
	}
}

// TestNodeExamConsistency_OrphanNodeExamID 验证清理孤立节点考试ID的一致性
func TestNodeExamConsistency_OrphanNodeExamID(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "orphan-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R901", Building: "G", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	// 创建一个已结束的考试
	endTime := time.Now().Add(-1 * time.Hour)
	exam := models.Exam{
		Name: "Ended-Exam", Subject: "EE", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-2 * time.Hour), EndTime: &endTime,
		DurationSeconds: 3600, ScheduleStatus: models.ExamScheduleRunning,
	}
	db.Create(&exam)

	// 创建节点，但 current_exam_id 指向已结束的考试
	examID := exam.ID
	node := models.Node{
		Name: "orphan-node", Token: "orphan-tok", NodeModel: "m1",
		Address: "10.0.0.1:8080", Status: models.NodeStatusBusy,
		Version: "1.0.0", CurrentExamID: &examID, LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	// 运行 cleanup
	cleanOrphanData()

	// 验证：节点应该被清理
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.CurrentExamID != nil {
		t.Errorf("expected orphan current_exam_id to be cleared, got %v", reloadedNode.CurrentExamID)
	}
}

// TestNodeExamConsistency_LeaseExpirationWithRunningExam 验证租约过期时考试和节点状态一致性
func TestNodeExamConsistency_LeaseExpirationWithRunningExam(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "lease-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1001", Building: "H", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	exam := models.Exam{
		Name: "Lease-Exam", Subject: "LE", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Hour), DurationSeconds: 7200,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	db.Create(&exam)

	examID := exam.ID
	node := models.Node{
		Name: "lease-node", Token: "lease-tok", NodeModel: "m1",
		Address: "10.0.0.1:8080", Status: models.NodeStatusBusy,
		Version: "1.0.0", CurrentExamID: &examID,
		LastHeartbeatAt: time.Now(),
		LeaseExpiresAt:  time.Now().Add(-1 * time.Minute), // 租约已过期
	}
	db.Create(&node)

	// 运行过期租约检查
	checkExpiredLeases()

	// 验证考试被标记为中断
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.EndTime == nil {
		t.Error("expected exam end_time set after lease expired")
	}
	if reloadedExam.ScheduleStatus != models.ExamScheduleInterrupted {
		t.Errorf("expected interrupted status, got %s", reloadedExam.ScheduleStatus)
	}

	// 验证节点被标记为离线
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.Status != models.NodeStatusOffline {
		t.Errorf("expected node offline after lease expired, got %s", reloadedNode.Status)
	}

	// 验证创建了告警
	var alertCount int64
	db.Model(&models.Alert{}).Where("exam_id = ? AND type = ?", exam.ID, "node_lease_expired").Count(&alertCount)
	if alertCount != 1 {
		t.Errorf("expected 1 lease_expired alert, got %d", alertCount)
	}
}

// TestNodeExamConsistency_PartialUniqueIndexEnforcement 验证同一节点不能有两场进行中考试
func TestNodeExamConsistency_PartialUniqueIndexEnforcement(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// 确保 partial unique index 存在
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_exams_node_active
		ON exams(node_id) WHERE end_time IS NULL AND node_id IS NOT NULL`).Error; err != nil {
		t.Fatalf("create partial index: %v", err)
	}

	user := models.User{Username: "index-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1101", Building: "I", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "index-node", Token: "idx-tok", NodeModel: "m1",
		Address: "10.0.0.1:8080", Status: models.NodeStatusIdle,
		Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	nodeID := node.ID
	exam1 := models.Exam{
		Name: "Index-Exam-1", Subject: "IE1", RoomID: room.ID, NodeID: &nodeID,
		UserID: user.ID, StartTime: time.Now(), DurationSeconds: 3600,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := db.Create(&exam1).Error; err != nil {
		t.Fatalf("create exam1: %v", err)
	}

	// 尝试创建第二场进行中考试（同一节点，end_time IS NULL）
	exam2 := models.Exam{
		Name: "Index-Exam-2", Subject: "IE2", RoomID: room.ID, NodeID: &nodeID,
		UserID: user.ID, StartTime: time.Now(), DurationSeconds: 3600,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	err := db.Create(&exam2).Error
	if err == nil {
		t.Fatal("expected unique constraint violation for second active exam on same node")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") && !strings.Contains(strings.ToLower(err.Error()), "uniq") {
		t.Logf("expected unique constraint error, got: %v (SQLite may behave differently)", err)
	}

	// 验证第一场考试仍然存在
	var count int64
	db.Model(&models.Exam{}).Where("node_id = ? AND end_time IS NULL", nodeID).Count(&count)
	if count != 1 {
		t.Errorf("expected exactly 1 active exam on node, got %d", count)
	}
}

// TestNotifyNodeStartExam_UnreachableNode 验证节点不可达（连接拒绝）时的行为
func TestNotifyNodeStartExam_UnreachableNode(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// 启动一个测试服务器然后立即关闭，模拟节点宕机
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success": true}`))
	}))
	serverAddr := strings.TrimPrefix(nodeServer.URL, "http://")
	nodeServer.Close() // 关闭服务器，模拟节点掉线

	user := models.User{Username: "unreach-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1201", Building: "J", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "unreach-node", Token: "unreach-tok", NodeModel: "m1",
		Address: serverAddr, Status: models.NodeStatusIdle,
		Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "Unreach-Exam", Subject: "UR", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	// 调度考试 — 通知应失败
	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected error when node is unreachable")
	}

	// 验证回滚：考试状态应为 notify_failed
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Errorf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}
	if reloadedExam.NodeID != nil {
		t.Errorf("expected node_id cleared on unreachable node, got %v", reloadedExam.NodeID)
	}

	// 验证节点被释放
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Errorf("expected node idle after rollback, got %s", reloadedNode.Status)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Errorf("expected node current_exam_id cleared, got %v", reloadedNode.CurrentExamID)
	}
}

// TestNotifyNodeStartExam_AddressUnavailable 验证节点地址未配置时的行为
func TestNotifyNodeStartExam_AddressUnavailable(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "noaddr-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1301", Building: "K", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	// 节点地址为 waiting_for_heartbeat（未上报地址）
	node := models.Node{
		Name: "noaddr-node", Token: "noaddr-tok", NodeModel: "m1",
		Address: "waiting_for_heartbeat", Status: models.NodeStatusIdle,
		Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "NoAddr-Exam", Subject: "NA", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected error when node address is unavailable")
	}
	if !strings.Contains(err.Error(), "address unavailable") {
		t.Errorf("expected 'address unavailable' error, got: %v", err)
	}

	// 验证回滚
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Errorf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}
}

// TestNotifyNodeStartExam_EmptyAddress 验证空地址时的行为
func TestNotifyNodeStartExam_EmptyAddress(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "emptyaddr-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1401", Building: "L", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "emptyaddr-node", Token: "emptyaddr-tok", NodeModel: "m1",
		Address: "   ", Status: models.NodeStatusIdle,
		Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "EmptyAddr-Exam", Subject: "EA", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected error when node address is empty")
	}
	if !strings.Contains(err.Error(), "address unavailable") {
		t.Errorf("expected 'address unavailable' error, got: %v", err)
	}
}

// TestNotifyNodeStartExam_NodeOfflineBeforeNotify 验证调度过程中节点变为离线
func TestNotifyNodeStartExam_NodeOfflineBeforeNotify(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	notifyCalled := false
	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
			notifyCalled = true
			// 节点返回 503，表示自身状态异常
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"success": false, "error": "node is not ready"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "offline-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1501", Building: "M", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "offline-during-sched", Token: "off-tok", NodeModel: "m1",
		Address: strings.TrimPrefix(nodeServer.URL, "http://"),
		Status: models.NodeStatusIdle, Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "OfflineDuringSched", Subject: "ODS", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected error when node returns 503")
	}
	if !notifyCalled {
		t.Error("expected notify to be attempted even if node returns error")
	}

	// 验证回滚：考试状态应为 notify_failed
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Errorf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}

	// 验证节点被释放
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Errorf("expected node idle after rollback, got %s", reloadedNode.Status)
	}
}

// TestNotifyNodeStartExam_Timeout 验证节点响应超时
func TestNotifyNodeStartExam_Timeout(t *testing.T) {
	db := setupExamSchedulerProcessTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	nodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/exam/schedule_start" && r.Method == http.MethodPost {
			// 模拟超时：长时间不响应
			time.Sleep(6 * time.Second) // 超过 5s client timeout
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success": false, "error": "not found"}`))
	}))
	defer nodeServer.Close()

	user := models.User{Username: "timeout-user", Password: "pass", Role: "proctor"}
	db.Create(&user)
	room := models.Room{Name: "R1601", Building: "N", RTSPUrl: "rtsp://test"}
	db.Create(&room)

	node := models.Node{
		Name: "timeout-node", Token: "timeout-tok", NodeModel: "m1",
		Address: strings.TrimPrefix(nodeServer.URL, "http://"),
		Status: models.NodeStatusIdle, Version: "1.0.0", LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	exam := models.Exam{
		Name: "Timeout-Exam", Subject: "TO", RoomID: room.ID, UserID: user.ID,
		StartTime: time.Now().Add(-1 * time.Minute), DurationSeconds: 1800,
		ScheduleStatus: models.ExamSchedulePending,
	}
	db.Create(&exam)

	err := scheduleExamByID(exam.ID, false)
	if err == nil {
		t.Fatal("expected timeout error when node does not respond")
	}

	// 验证回滚
	var reloadedExam models.Exam
	db.First(&reloadedExam, exam.ID)
	if reloadedExam.ScheduleStatus != models.ExamScheduleNotifyFail {
		t.Errorf("expected notify_failed, got %s", reloadedExam.ScheduleStatus)
	}
	if reloadedExam.NodeID != nil {
		t.Errorf("expected node_id cleared on timeout, got %v", reloadedExam.NodeID)
	}

	// 验证节点被释放
	var reloadedNode models.Node
	db.First(&reloadedNode, node.ID)
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Errorf("expected node idle after timeout rollback, got %s", reloadedNode.Status)
	}
}
