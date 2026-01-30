package test

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cc/models"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Set the global DB for models
	models.DB = db

	return db
}

func TestUserInsert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Insert test users
	hashed1, _ := bcrypt.GenerateFromPassword([]byte("password1"), bcrypt.DefaultCost)
	user1 := models.User{
		Username: "testuser1",
		Password: string(hashed1),
		Role:     models.Proctor,
	}

	hashed2, _ := bcrypt.GenerateFromPassword([]byte("password2"), bcrypt.DefaultCost)
	user2 := models.User{
		Username: "testuser2",
		Password: string(hashed2),
		Role:     models.Admin,
	}

	if err := db.Create(&user1).Error; err != nil {
		t.Errorf("failed to create user1: %v", err)
	}

	if err := db.Create(&user2).Error; err != nil {
		t.Errorf("failed to create user2: %v", err)
	}

	// Verify insertion
	var count int64
	db.Model(&models.User{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}

	var retrieved models.User
	if err := db.Where("username = ?", "testuser1").First(&retrieved).Error; err != nil {
		t.Errorf("failed to retrieve user1: %v", err)
	}
	if retrieved.Role != models.Proctor {
		t.Errorf("expected role proctor, got %s", retrieved.Role)
	}
}
