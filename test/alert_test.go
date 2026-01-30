package test

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cc/models"
)

func setupTestDBForAlert(t *testing.T) *gorm.DB {
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

func TestAlertInsert(t *testing.T) {
	db := setupTestDBForAlert(t)
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

	startTime := time.Now()
	endTime := startTime.Add(2 * time.Hour)
	exam := models.Exam{
		Name:          "Math Exam",
		Subject:       "Mathematics",
		RoomID:        room.ID,
		NodeID:        node.ID,
		UserID:        user.ID,
		StartTime:     startTime,
		EndTime:       &endTime,
		ExamineeCount: 30,
	}
	db.Create(&exam)

	// Insert test alerts
	alert1 := models.Alert{
		ExamID:      exam.ID,
		Type:        models.AlertTypePhoneCheating,
		SeatNumber:  "A01",
		X:           100.5,
		Y:           200.3,
		Message:     "Detected phone usage",
		PicturePath: "/path/to/picture1.jpg",
	}

	alert2 := models.Alert{
		ExamID:      exam.ID,
		Type:        models.AlertTypeLookAround,
		SeatNumber:  "B05",
		X:           150.0,
		Y:           250.0,
		Message:     "Student looking around",
		PicturePath: "/path/to/picture2.jpg",
	}

	if err := db.Create(&alert1).Error; err != nil {
		t.Errorf("failed to create alert1: %v", err)
	}

	if err := db.Create(&alert2).Error; err != nil {
		t.Errorf("failed to create alert2: %v", err)
	}

	// Verify insertion
	var count int64
	db.Model(&models.Alert{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 alerts, got %d", count)
	}

	var retrieved models.Alert
	if err := db.Where("seat_number = ?", "A01").First(&retrieved).Error; err != nil {
		t.Errorf("failed to retrieve alert1: %v", err)
	}
	if retrieved.Type != models.AlertTypePhoneCheating {
		t.Errorf("expected type phone_cheating, got %s", retrieved.Type)
	}
}
