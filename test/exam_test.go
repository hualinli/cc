package test

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cc/models"
)

func setupTestDBForExam(t *testing.T) *gorm.DB {
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

func TestExamInsert(t *testing.T) {
	db := setupTestDBForExam(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Insert prerequisite data
	hashed, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
	user := models.User{
		Username: "proctor1",
		Password: string(hashed),
		Role:     models.Proctor,
	}
	db.Create(&user)

	room := models.Room{
		Building: "Building A",
		Name:     "Room 101",
		RTSPUrl:  "rtsp://example.com/stream1",
	}
	db.Create(&room)

	node := models.Node{
		Name:            "Node1",
		Token:           "token123",
		Model:           "ModelA",
		Address:         "192.168.1.1",
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		ConfigVersion:   1,
		LastHeartbeatAt: time.Now(),
	}
	db.Create(&node)

	// Insert test exams
	startTime := time.Now()
	endTime := startTime.Add(2 * time.Hour)
	nodeID := node.ID

	exam1 := models.Exam{
		Name:          "Math Exam",
		Subject:       "Mathematics",
		RoomID:        room.ID,
		NodeID:        &nodeID,
		UserID:        user.ID,
		StartTime:     startTime,
		EndTime:       &endTime,
		ExamineeCount: 30,
	}

	nodeID2 := node.ID
	exam2 := models.Exam{
		Name:          "English Exam",
		Subject:       "English",
		RoomID:        room.ID,
		NodeID:        &nodeID2,
		UserID:        user.ID,
		StartTime:     startTime.Add(24 * time.Hour),
		EndTime:       nil,
		ExamineeCount: 25,
	}

	if err := db.Create(&exam1).Error; err != nil {
		t.Errorf("failed to create exam1: %v", err)
	}

	if err := db.Create(&exam2).Error; err != nil {
		t.Errorf("failed to create exam2: %v", err)
	}

	// Verify insertion
	var count int64
	db.Model(&models.Exam{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 exams, got %d", count)
	}

	var retrieved models.Exam
	if err := db.Where("name = ?", "Math Exam").First(&retrieved).Error; err != nil {
		t.Errorf("failed to retrieve exam1: %v", err)
	}
	if retrieved.Subject != "Mathematics" {
		t.Errorf("expected subject Mathematics, got %s", retrieved.Subject)
	}
}
