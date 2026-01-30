package test

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cc/models"
)

func setupTestDBForNode(t *testing.T) *gorm.DB {
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

func TestNodeInsert(t *testing.T) {
	db := setupTestDBForNode(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Insert test nodes
	node1 := models.Node{
		Name:            "Node1",
		Token:           "token123",
		Model:           "ModelA",
		Address:         "192.168.1.1",
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		ConfigVersion:   1,
		LastHeartbeatAt: time.Now(),
	}

	node2 := models.Node{
		Name:            "Node2",
		Token:           "token456",
		Model:           "ModelB",
		Address:         "192.168.1.2",
		Status:          models.NodeStatusBusy,
		Version:         "1.1.0",
		ConfigVersion:   2,
		LastHeartbeatAt: time.Now(),
	}

	if err := db.Create(&node1).Error; err != nil {
		t.Errorf("failed to create node1: %v", err)
	}

	if err := db.Create(&node2).Error; err != nil {
		t.Errorf("failed to create node2: %v", err)
	}

	// Verify insertion
	var count int64
	db.Model(&models.Node{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 nodes, got %d", count)
	}

	var retrieved models.Node
	if err := db.Where("name = ?", "Node1").First(&retrieved).Error; err != nil {
		t.Errorf("failed to retrieve node1: %v", err)
	}
	if retrieved.Status != models.NodeStatusIdle {
		t.Errorf("expected status idle, got %s", retrieved.Status)
	}
}
