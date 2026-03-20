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

func TestCleanupStaleExams_DoesNotCloseRunningOnIdleNode(t *testing.T) {
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
		Name:            "n1",
		Token:           "t1",
		Model:           "m1",
		Address:         "127.0.0.1:8002",
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create node failed: %v", err)
	}

	nodeID := node.ID
	exam := models.Exam{
		Name:            "e1",
		Subject:         "math",
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

	cleanupStaleExams()

	var refreshed models.Exam
	if err := db.First(&refreshed, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshed.EndTime != nil {
		t.Fatalf("expected exam not to be auto-closed on idle node")
	}
}

func TestCleanupStaleExams_ClosesRunningOnOfflineNode(t *testing.T) {
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
		Name:            "n2",
		Token:           "t2",
		Model:           "m1",
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
		Name:            "e2",
		Subject:         "english",
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

	cleanupStaleExams()

	var refreshed models.Exam
	if err := db.First(&refreshed, exam.ID).Error; err != nil {
		t.Fatalf("reload exam failed: %v", err)
	}
	if refreshed.EndTime == nil {
		t.Fatalf("expected exam to be auto-closed on offline node")
	}
}
