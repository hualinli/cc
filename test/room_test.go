package test

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cc/models"
)

func setupTestDBForRoom(t *testing.T) *gorm.DB {
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

func TestRoomInsert(t *testing.T) {
	db := setupTestDBForRoom(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Insert test rooms
	room1 := models.Room{
		Building: "Building A",
		Name:     "Room 101",
		RTSPUrl:  "rtsp://example.com/stream1",
	}

	room2 := models.Room{
		Building: "Building B",
		Name:     "Room 202",
		RTSPUrl:  "rtsp://example.com/stream2",
	}

	if err := db.Create(&room1).Error; err != nil {
		t.Errorf("failed to create room1: %v", err)
	}

	if err := db.Create(&room2).Error; err != nil {
		t.Errorf("failed to create room2: %v", err)
	}

	// Verify insertion
	var count int64
	db.Model(&models.Room{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 rooms, got %d", count)
	}

	var retrieved models.Room
	if err := db.Where("name = ?", "Room 101").First(&retrieved).Error; err != nil {
		t.Errorf("failed to retrieve room1: %v", err)
	}
	if retrieved.Building != "Building A" {
		t.Errorf("expected building Building A, got %s", retrieved.Building)
	}
}
