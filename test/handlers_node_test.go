package test

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/securecookie"

	"cc/handlers"
	"cc/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	gob.Register(uint(0))
	gob.Register("")
}

func setupTestDBForHandlers(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})
	if err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	// Create base data for foreign key constraints
	db.Create(&models.User{Username: "admin", Password: "password", Role: "admin"})
	db.Create(&models.Room{Name: "Base Room", Building: "Base", RTSPUrl: "rtsp://base"})

	models.DB = db
	return db
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("mysession", store))

	r.GET("/nodes", handlers.ListNodes)
	r.GET("/nodes/:id", handlers.GetNode)
	r.POST("/nodes", handlers.CreateNode)
	r.DELETE("/nodes/:id", handlers.DeleteNode)
	r.PUT("/nodes/:id", handlers.UpdateNode)

	return r
}

// Helper function to create a request with session cookie
func createRequestWithSession(method, url string, body *bytes.Buffer, userID uint, role string) *http.Request {
	var reader io.Reader
	if body != nil {
		reader = body
	}
	req, _ := http.NewRequest(method, url, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Create session data
	sessionData := map[interface{}]interface{}{
		"user_id": userID,
		"role":    role,
	}

	// Use securecookie to encode
	sc := securecookie.New([]byte("secret"), nil)
	encoded, err := sc.Encode("mysession", sessionData)
	if err != nil {
		panic(err)
	}

	// Set cookie in request
	cookie := &http.Cookie{
		Name:  "mysession",
		Value: encoded,
		Path:  "/",
	}
	req.AddCookie(cookie)

	return req
}

func TestCreateNode(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	r := setupRouter()

	input := map[string]string{
		"name":    "New Node",
		"model":   "NewModel",
		"address": "192.168.1.1",
	}
	jsonData, _ := json.Marshal(input)

	req, _ := http.NewRequest("POST", "/nodes", bytes.NewBuffer(jsonData))
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

	// Verify node was created
	var node models.Node
	db.First(&node)
	if node.Name != "New Node" {
		t.Errorf("Expected node name 'New Node', got %s", node.Name)
	}
}

func TestDeleteNode(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test node
	node := models.Node{
		Name:    "Node to Delete",
		Token:   "token",
		Model:   "Model",
		Address: "127.0.0.1",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)

	r := setupRouter()

	req, _ := http.NewRequest("DELETE", "/nodes/1", nil)
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

	// Verify node was deleted
	var count int64
	db.Model(&models.Node{}).Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 nodes, got %d", count)
	}
}

func TestDeleteNodeWithExam(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test node
	node := models.Node{
		Name:    "Node with Exam",
		Token:   "token",
		Model:   "Model",
		Address: "127.0.0.1",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)

	// Create test exam that references the node
	exam := models.Exam{
		Name:    "Test Exam",
		Subject: "Math",
		RoomID:  1,
		NodeID:  node.ID,
		UserID:  1,
	}
	db.Create(&exam)

	r := setupRouter()

	req, _ := http.NewRequest("DELETE", "/nodes/1", nil)
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

	// Verify node was NOT deleted
	var count int64
	db.Model(&models.Node{}).Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 node, got %d", count)
	}
}

func TestUpdateNode(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test node
	node := models.Node{
		Name:    "Old Name",
		Token:   "token",
		Model:   "OldModel",
		Address: "127.0.0.1",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)

	r := setupRouter()

	input := map[string]string{
		"name":    "Updated Name",
		"model":   "UpdatedModel",
		"address": "192.168.1.2",
	}
	jsonData, _ := json.Marshal(input)

	req, _ := http.NewRequest("PUT", "/nodes/1", bytes.NewBuffer(jsonData))
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

	// Verify node was updated
	var updatedNode models.Node
	db.First(&updatedNode)
	if updatedNode.Name != "Updated Name" {
		t.Errorf("Expected updated name 'Updated Name', got %s", updatedNode.Name)
	}
}

// TestListNodes requires session mocking, which is complex in httptest
// For now, skipping or using a simplified version

func TestListNodes(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test data
	node := models.Node{
		Name:    "Test Node",
		Token:   "testtoken",
		Model:   "TestModel",
		Address: "127.0.0.1",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)

	r := setupRouter()

	req := createRequestWithSession("GET", "/nodes", nil, 1, "admin")
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
	if len(data) != 1 {
		t.Errorf("Expected 1 node, got %d", len(data))
	}
}

func TestGetNodes(t *testing.T) {
	db := setupTestDBForHandlers(t)
	defer db.Migrator().DropTable(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{})

	// Create test node
	node := models.Node{
		Name:    "Test Node",
		Token:   "testtoken",
		Model:   "TestModel",
		Address: "127.0.0.1",
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}
	db.Create(&node)

	r := setupRouter()

	req := createRequestWithSession("GET", "/nodes/1", nil, 1, "admin")
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
	if data["name"] != "Test Node" {
		t.Errorf("Expected node name 'Test Node', got %v", data["name"])
	}
}
