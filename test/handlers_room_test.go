package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"cc/handlers"
	"cc/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForRoomHandlers(t *testing.T) *gorm.DB {
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

func setupRoomRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("mysession", store))

	r.GET("/rooms", handlers.ListRooms)
	r.GET("/rooms/:id", handlers.GetRoom)
	r.POST("/rooms", handlers.CreateRoom)
	r.DELETE("/rooms/:id", handlers.DeleteRoom)
	r.PUT("/rooms/:id", handlers.UpdateRoom)

	return r
}

func TestCreateRoom(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	r := setupRoomRouter()

	input := map[string]string{
		"name":     "Test Room",
		"building": "Building A",
		"rtsp_url": "rtsp://example.com/stream",
	}
	jsonData, _ := json.Marshal(input)

	req, _ := http.NewRequest("POST", "/rooms", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Errorf("Expected success true, got %v", response["success"])
	}

	// Verify room was created
	var room models.Room
	db.First(&room)
	if room.Name != "Test Room" {
		t.Errorf("Expected room name 'Test Room', got %s", room.Name)
	}
}

func TestDeleteRoom(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test room
	room := models.Room{
		Name:     "Room to Delete",
		Building: "Building A",
		RTSPUrl:  "rtsp://example.com/stream",
	}
	db.Create(&room)

	r := setupRoomRouter()

	req, _ := http.NewRequest("DELETE", "/rooms/1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Errorf("Expected success true, got %v", response["success"])
	}

	// Verify room was deleted
	var count int64
	db.Model(&models.Room{}).Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 rooms, got %d", count)
	}
}

func TestDeleteRoomWithExam(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test room
	room := models.Room{
		Name:     "Room with Exam",
		Building: "Building A",
		RTSPUrl:  "rtsp://example.com/stream",
	}
	db.Create(&room)

	// Create test exam that references the room
	exam := models.Exam{
		Name:    "Test Exam",
		Subject: "Math",
		RoomID:  room.ID,
		NodeID:  1,
		UserID:  1,
	}
	db.Create(&exam)

	r := setupRoomRouter()

	req, _ := http.NewRequest("DELETE", "/rooms/1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected status 409, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != false {
		t.Errorf("Expected success false, got %v", response["success"])
	}

	// Verify room was NOT deleted
	var count int64
	db.Model(&models.Room{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 room, got %d", count)
	}
}

func TestListRooms(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test rooms
	room1 := models.Room{
		Name:     "Room 1",
		Building: "Building A",
		RTSPUrl:  "rtsp://example.com/stream1",
	}
	room2 := models.Room{
		Name:     "Room 2",
		Building: "Building B",
		RTSPUrl:  "rtsp://example.com/stream2",
	}
	db.Create(&room1)
	db.Create(&room2)

	r := setupRoomRouter()

	req, _ := http.NewRequest("GET", "/rooms", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Errorf("Expected success true, got %v", response["success"])
	}

	data := response["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("Expected 2 rooms, got %d", len(data))
	}
}

func TestGetRoom(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test room
	room := models.Room{
		Name:     "Test Room",
		Building: "Building A",
		RTSPUrl:  "rtsp://example.com/stream",
	}
	db.Create(&room)

	r := setupRoomRouter()

	req, _ := http.NewRequest("GET", "/rooms/1", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Errorf("Expected success true, got %v", response["success"])
	}

	data := response["data"].(map[string]interface{})
	if data["name"] != "Test Room" {
		t.Errorf("Expected room name 'Test Room', got %v", data["name"])
	}
	if data["building"] != "Building A" {
		t.Errorf("Expected building 'Building A', got %v", data["building"])
	}
}

func TestUpdateRoom(t *testing.T) {
	db := setupTestDBForRoomHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test room
	room := models.Room{
		Name:     "Old Name",
		Building: "Old Building",
		RTSPUrl:  "rtsp://old.example.com/stream",
	}
	db.Create(&room)

	r := setupRoomRouter()

	input := map[string]string{
		"name":     "Updated Name",
		"building": "Updated Building",
		"rtsp_url": "rtsp://updated.example.com/stream",
	}
	jsonData, _ := json.Marshal(input)

	req, _ := http.NewRequest("PUT", "/rooms/1", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Errorf("Expected success true, got %v", response["success"])
	}

	// Verify room was updated
	var updatedRoom models.Room
	db.First(&updatedRoom)
	if updatedRoom.Name != "Updated Name" {
		t.Errorf("Expected updated name 'Updated Name', got %s", updatedRoom.Name)
	}
	if updatedRoom.Building != "Updated Building" {
		t.Errorf("Expected updated building 'Updated Building', got %s", updatedRoom.Building)
	}
	if updatedRoom.RTSPUrl != "rtsp://updated.example.com/stream" {
		t.Errorf("Expected updated RTSP URL 'rtsp://updated.example.com/stream', got %s", updatedRoom.RTSPUrl)
	}
}
