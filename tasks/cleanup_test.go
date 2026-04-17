package tasks

import (
	"cc/models"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCleanupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	models.DB = db
	return db
}

func TestCleanupStaleNodes_IdleToOffline(t *testing.T) {
	db := setupCleanupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "u1", Password: "p", Role: "proctor"}
	room := models.Room{Name: "r1", Building: "b1", RTSPUrl: "rtsp://x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:                  "n1",
		Token:                 "t1",
		NodeModel:             "m1",
		Address:               "127.0.0.1:8002",
		Status:                models.NodeStatusIdle,
		Version:               "1.0.0",
		CurrentUserID:         &user.ID,
		CurrentUserOccupiedAt: ptrTime(time.Now().Add(-3 * time.Minute)),
		LastHeartbeatAt:       time.Now().Add(-3 * time.Minute),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	cleanupStaleNodes()

	var refreshed models.Node
	if err := db.First(&refreshed, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if refreshed.Status != models.NodeStatusOffline {
		t.Fatalf("expected node status offline, got %s", refreshed.Status)
	}
	if refreshed.CurrentUserID != nil {
		t.Fatal("expected current_user_id to be cleared on idle->offline")
	}
	if refreshed.CurrentUserOccupiedAt != nil {
		t.Fatal("expected current_user_occupied_at to be cleared on idle->offline")
	}
}

func TestCleanupStaleNodes_BusyNoExamReleases(t *testing.T) {
	db := setupCleanupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "u2", Password: "p", Role: "proctor"}
	room := models.Room{Name: "r2", Building: "b1", RTSPUrl: "rtsp://x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:                  "n2",
		Token:                 "t2",
		NodeModel:             "m1",
		Address:               "127.0.0.1:8002",
		Status:                models.NodeStatusBusy,
		Version:               "1.0.0",
		CurrentUserID:         &user.ID,
		CurrentUserOccupiedAt: ptrTime(time.Now().Add(-3 * time.Minute)),
		LastHeartbeatAt:       time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	cleanupStaleNodes()

	var refreshed models.Node
	if err := db.First(&refreshed, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if refreshed.Status != models.NodeStatusIdle {
		t.Fatalf("expected node status idle, got %s", refreshed.Status)
	}
	if refreshed.CurrentUserID != nil {
		t.Fatal("expected current_user_id to be cleared on busy-no-exam release")
	}
	if refreshed.CurrentUserOccupiedAt != nil {
		t.Fatal("expected current_user_occupied_at to be cleared on busy-no-exam release")
	}
}

func TestCleanupStaleOfflineRunningNode_TerminatesExam(t *testing.T) {
	db := setupCleanupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "u3", Password: "p", Role: "proctor"}
	room := models.Room{Name: "r3", Building: "b1", RTSPUrl: "rtsp://x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "n3",
		Token:           "t3",
		NodeModel:       "m1",
		Address:         "127.0.0.1:8002",
		Status:          models.NodeStatusBusy,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	nodeID := node.ID
	exam := models.Exam{
		Name:            "e3",
		Subject:         "physics",
		RoomID:          room.ID,
		NodeID:          &nodeID,
		UserID:          user.ID,
		DurationSeconds: 3600,
		StartTime:       time.Now().Add(-2 * time.Minute),
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	if err := db.Model(&models.Node{}).
		Where("id = ?", node.ID).
		Updates(map[string]any{
			"current_exam_id":          exam.ID,
			"current_user_id":          user.ID,
			"current_user_occupied_at": time.Now().Add(-3 * time.Minute),
		}).Error; err != nil {
		t.Fatalf("bind node occupation failed: %v", err)
	}

	staleHeartbeat := time.Now().Add(-3 * time.Minute)
	if err := db.Exec("UPDATE nodes SET last_heartbeat_at = ? WHERE id = ?", staleHeartbeat, node.ID).Error; err != nil {
		t.Fatalf("set stale heartbeat failed: %v", err)
	}

	// case3: 运行中节点掉线（busy 且无心跳）
	// 期望：节点先被置为 offline 并清理占用，随后关联运行中考试被自动终止并写入掉线原因。
	cleanupStaleNodes()
	cleanupStaleExams()

	var refreshedNode models.Node
	if err := db.First(&refreshedNode, node.ID).Error; err != nil {
		t.Fatalf("reload node failed: %v", err)
	}
	if refreshedNode.Status != models.NodeStatusOffline {
		t.Fatalf("expected node status offline, got %s", refreshedNode.Status)
	}
	if refreshedNode.CurrentExamID != nil {
		t.Fatal("expected current_exam_id to be cleared")
	}
	if refreshedNode.CurrentUserID != nil {
		t.Fatal("expected current_user_id to be cleared")
	}

	var refreshedExam models.Exam
	if err := db.First(&refreshedExam, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshedExam.EndTime == nil {
		t.Fatal("expected running exam to be terminated")
	}
	if refreshedExam.ScheduleError != "由于节点掉线自动终止" {
		t.Fatalf("expected schedule_error to be offline reason, got %q", refreshedExam.ScheduleError)
	}
}

func TestCleanupStaleExams_NoNodeLinkageTerminatesExam(t *testing.T) {
	db := setupCleanupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "u4", Password: "p", Role: "proctor"}
	room := models.Room{Name: "r4", Building: "b1", RTSPUrl: "rtsp://x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	// 覆盖僵尸场景：运行中考试没有节点关联，cleanup 需要主动收敛。
	exam := models.Exam{
		Name:            "e4",
		Subject:         "chemistry",
		RoomID:          room.ID,
		NodeID:          nil,
		UserID:          user.ID,
		DurationSeconds: 3600,
		StartTime:       time.Now().Add(-5 * time.Minute),
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	cleanupStaleExams()

	var refreshedExam models.Exam
	if err := db.First(&refreshedExam, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshedExam.EndTime == nil {
		t.Fatal("expected running exam without node linkage to be terminated")
	}
	if refreshedExam.ScheduleError != "由于节点关联缺失自动终止" {
		t.Fatalf("expected schedule_error to be missing-link reason, got %q", refreshedExam.ScheduleError)
	}
}

func TestCleanupStaleExams_NodeUpdateFailureRollsBackAndRetrySucceeds(t *testing.T) {
	db := setupCleanupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	user := models.User{Username: "u5", Password: "p", Role: "proctor"}
	room := models.Room{Name: "r5", Building: "b1", RTSPUrl: "rtsp://x"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatalf("create room failed: %v", err)
	}

	node := models.Node{
		Name:            "n5",
		Token:           "t5",
		NodeModel:       "m1",
		Address:         "127.0.0.1:8002",
		Status:          models.NodeStatusOffline,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now().Add(-5 * time.Minute),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	nodeID := node.ID
	exam := models.Exam{
		Name:            "e5",
		Subject:         "biology",
		RoomID:          room.ID,
		NodeID:          &nodeID,
		UserID:          user.ID,
		DurationSeconds: 3600,
		StartTime:       time.Now().Add(-5 * time.Minute),
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := db.Create(&exam).Error; err != nil {
		t.Fatalf("create exam failed: %v", err)
	}

	if err := db.Model(&models.Node{}).
		Where("id = ?", node.ID).
		Updates(map[string]any{
			"current_exam_id":          exam.ID,
			"current_user_id":          user.ID,
			"current_user_occupied_at": time.Now().Add(-5 * time.Minute),
		}).Error; err != nil {
		t.Fatalf("bind node occupation failed: %v", err)
	}

	// 模拟节点更新失败：第一次 cleanup 时，node update 失败应触发事务回滚。
	if err := db.Exec(`
		CREATE TRIGGER block_nodes_update_before_retry
		BEFORE UPDATE ON nodes
		BEGIN
			SELECT RAISE(ABORT, 'block node update');
		END;
	`).Error; err != nil {
		t.Fatalf("create trigger failed: %v", err)
	}

	cleanupStaleExams()

	var afterFailedTry models.Exam
	if err := db.First(&afterFailedTry, exam.ID).Error; err != nil {
		t.Fatalf("reload exam after failed try failed: %v", err)
	}
	if afterFailedTry.EndTime != nil {
		t.Fatal("expected transaction rollback when node update fails")
	}

	if err := db.Exec("DROP TRIGGER block_nodes_update_before_retry").Error; err != nil {
		t.Fatalf("drop trigger failed: %v", err)
	}

	cleanupStaleExams()

	var afterRetry models.Exam
	if err := db.First(&afterRetry, exam.ID).Error; err != nil {
		t.Fatalf("reload exam after retry failed: %v", err)
	}
	if afterRetry.EndTime == nil {
		t.Fatal("expected retry to terminate exam successfully")
	}
	if afterRetry.ScheduleError != "由于节点掉线自动终止" {
		t.Fatalf("expected schedule_error to be offline reason after retry, got %q", afterRetry.ScheduleError)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
