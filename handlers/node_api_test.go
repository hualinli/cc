package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupNodeAPIHandlerTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	models.DB = db

	return func() {
		models.DB = oldDB
		_ = sqlDB.Close()
	}
}

func seedNodeAPIUser(t *testing.T) models.User {
	t.Helper()
	user := models.User{Username: "node-user", Password: "x", Role: models.Proctor}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedNodeAPIRoom(t *testing.T) models.Room {
	t.Helper()
	room := models.Room{Name: "R201", Building: "Main", RTSPUrl: "rtsp://cam"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedNodeAPIModel(t *testing.T, name string) models.Node {
	t.Helper()
	node := models.Node{Name: name, Token: "token-" + name, Status: models.NodeStatusIdle}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

func decodeNodeAPIResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func performNodeAPIJSONRequest(t *testing.T, method, path, body string, nodeID uint, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, func(c *gin.Context) {
		c.Set("node_id", nodeID)
		handler(c)
	})

	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSyncTaskStart_AssignedExamWrongNodeReturnsForbidden(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node1 := seedNodeAPIModel(t, "node-1")
	node2 := seedNodeAPIModel(t, "node-2")
	node2ID := node2.ID
	exam := models.Exam{
		Name:           "math exam",
		Subject:        "math",
		RoomID:         room.ID,
		NodeID:         &node2ID,
		UserID:         user.ID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"action":"start","room_id":%d,"subject":"math","start_time":"%s","exam_id":%d}`,
		room.ID, time.Now().Format(time.RFC3339), exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node1.ID, SyncTask)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "exam_id 不属于当前节点" {
		t.Fatalf("expected exam ownership error, got %v", resp["error"])
	}
}

func TestSyncTaskSync_RejectsOtherNodeExam(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node1 := seedNodeAPIModel(t, "node-1")
	node2 := seedNodeAPIModel(t, "node-2")
	node2ID := node2.ID
	exam := models.Exam{
		Name:           "physics exam",
		Subject:        "physics",
		RoomID:         room.ID,
		NodeID:         &node2ID,
		UserID:         user.ID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamScheduleRunning,
		ExamineeCount:  10,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"action":"sync","exam_id":%d,"examinee_count":99}`,
		exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node1.ID, SyncTask)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "exam_id 不属于当前节点" {
		t.Fatalf("expected exam ownership error, got %v", resp["error"])
	}

	var reloaded models.Exam
	if err := models.DB.First(&reloaded, exam.ID).Error; err != nil {
		t.Fatalf("failed to reload exam: %v", err)
	}
	if reloaded.ExamineeCount != 10 {
		t.Fatalf("expected examinee_count to remain 10, got %d", reloaded.ExamineeCount)
	}
}

func TestSyncTaskStart_CreateExamSuccess(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "start-create-node")

	now := time.Now()
	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
		"status":                   models.NodeStatusIdle,
	}).Error; err != nil {
		t.Fatalf("failed to occupy node: %v", err)
	}

	body := fmt.Sprintf(`{"action":"start","room_id":%d,"subject":"math","start_time":"%s","duration_minutes":90,"examinee_count":30}`,
		room.ID, now.Format(time.RFC3339))
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["success"] != true {
		t.Fatalf("expected success true, got %v", resp["success"])
	}

	examIDValue, ok := resp["exam_id"].(float64)
	if !ok || uint(examIDValue) == 0 {
		t.Fatalf("expected valid exam_id, got %v", resp["exam_id"])
	}
	examID := uint(examIDValue)

	var exam models.Exam
	if err := models.DB.First(&exam, examID).Error; err != nil {
		t.Fatalf("failed to load created exam: %v", err)
	}
	if exam.NodeID == nil || *exam.NodeID != node.ID {
		t.Fatalf("expected exam node_id %d, got %v", node.ID, exam.NodeID)
	}
	if exam.UserID != user.ID {
		t.Fatalf("expected exam user_id %d, got %d", user.ID, exam.UserID)
	}

	var reloaded models.Node
	if err := models.DB.First(&reloaded, node.ID).Error; err != nil {
		t.Fatalf("failed to reload node: %v", err)
	}
	if reloaded.CurrentExamID == nil || *reloaded.CurrentExamID != examID {
		t.Fatalf("expected node current_exam_id %d, got %v", examID, reloaded.CurrentExamID)
	}
	if reloaded.Status != models.NodeStatusBusy {
		t.Fatalf("expected node status busy, got %s", reloaded.Status)
	}
}

func TestSyncTaskStart_NoExamIDWithActiveExamReturnsSameID(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "start-existing-active-node")
	nodeID := node.ID
	now := time.Now()
	exam := models.Exam{
		Name:            "active exam",
		Subject:         "math",
		RoomID:          room.ID,
		NodeID:          &nodeID,
		UserID:          user.ID,
		DurationSeconds: 3600,
		StartTime:       now,
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed active exam: %v", err)
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
		"status":                   models.NodeStatusBusy,
		"current_exam_id":          exam.ID,
	}).Error; err != nil {
		t.Fatalf("failed to occupy node: %v", err)
	}

	body := fmt.Sprintf(`{"action":"start","room_id":%d,"subject":"math","start_time":"%s","duration_minutes":120}`,
		room.ID, now.Format(time.RFC3339))
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["exam_id"] != float64(exam.ID) {
		t.Fatalf("expected existing exam_id %d, got %v", exam.ID, resp["exam_id"])
	}

	var examCount int64
	if err := models.DB.Model(&models.Exam{}).Where("node_id = ? AND end_time IS NULL", node.ID).Count(&examCount).Error; err != nil {
		t.Fatalf("failed to count active exams: %v", err)
	}
	if examCount != 1 {
		t.Fatalf("expected exactly 1 active exam, got %d", examCount)
	}
}

func TestSyncTaskStart_IdempotentAssignedExamReturnsSameID(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "start-idempotent-node")
	nodeID := node.ID
	exam := models.Exam{
		Name:           "assigned exam",
		Subject:        "math",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"action":"start","exam_id":%d}`, exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["exam_id"] != float64(exam.ID) {
		t.Fatalf("expected exam_id %d, got %v", exam.ID, resp["exam_id"])
	}
}

func TestSyncTaskStart_MissingRoomOrSubject(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	node := seedNodeAPIModel(t, "start-bad-request-node")
	body := `{"action":"start","room_id":0,"subject":""}`
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "缺少必要参数: room_id 或 subject" {
		t.Fatalf("expected missing params error, got %v", resp["error"])
	}
}

func TestSyncTaskStop_Success(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "stop-node")
	now := time.Now()
	nodeID := node.ID
	exam := models.Exam{
		Name:           "stop exam",
		Subject:        "physics",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      now,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"current_exam_id":          exam.ID,
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
		"status":                   models.NodeStatusBusy,
	}).Error; err != nil {
		t.Fatalf("failed to set node busy state: %v", err)
	}

	body := fmt.Sprintf(`{"action":"stop","exam_id":%d}`, exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloadedExam models.Exam
	if err := models.DB.First(&reloadedExam, exam.ID).Error; err != nil {
		t.Fatalf("failed to reload exam: %v", err)
	}
	if reloadedExam.EndTime == nil {
		t.Fatal("expected exam end_time to be set")
	}

	var reloadedNode models.Node
	if err := models.DB.First(&reloadedNode, node.ID).Error; err != nil {
		t.Fatalf("failed to reload node: %v", err)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Fatalf("expected current_exam_id nil, got %v", reloadedNode.CurrentExamID)
	}
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Fatalf("expected node status idle, got %s", reloadedNode.Status)
	}
}

func TestSyncTaskSync_Success(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "sync-success-node")
	nodeID := node.ID
	exam := models.Exam{
		Name:           "sync exam",
		Subject:        "chem",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamScheduleRunning,
		ExamineeCount:  12,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"action":"sync","exam_id":%d,"examinee_count":66}`, exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded models.Exam
	if err := models.DB.First(&reloaded, exam.ID).Error; err != nil {
		t.Fatalf("failed to reload exam: %v", err)
	}
	if reloaded.ExamineeCount != 66 {
		t.Fatalf("expected examinee_count 66, got %d", reloaded.ExamineeCount)
	}
}

func TestSyncTaskSync_EndedExamReturnsConflict(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "sync-ended-node")
	nodeID := node.ID
	endTime := time.Now().Add(-1 * time.Minute)
	exam := models.Exam{
		Name:           "ended exam",
		Subject:        "history",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      time.Now().Add(-2 * time.Hour),
		EndTime:        &endTime,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"action":"sync","exam_id":%d,"examinee_count":99}`, exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/sync-task", body, node.ID, SyncTask)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "考试已结束" {
		t.Fatalf("expected 考试已结束, got %v", resp["error"])
	}
}

func TestReportAlert_InvalidTypeReturnsBadRequest(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "node-1")
	nodeID := node.ID
	exam := models.Exam{
		Name:           "chem exam",
		Subject:        "chem",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      time.Now(),
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	body := fmt.Sprintf(`{"exam_id":%d,"type":"bad_type","seat_number":"A1"}`,
		exam.ID)
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/report-alert", body, node.ID, ReportAlert)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "type 无效" {
		t.Fatalf("expected type 无效, got %v", resp["error"])
	}

	var count int64
	if err := models.DB.Model(&models.Alert{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 alerts, got %d", count)
	}
}

func TestNodeHeartbeat_ConcurrentMultiNodeUpdatesStatusAndAddress(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	node1 := seedNodeAPIModel(t, "hb-node-1")
	node2 := seedNodeAPIModel(t, "hb-node-2")
	node3 := seedNodeAPIModel(t, "hb-node-3")

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/heartbeat", func(c *gin.Context) {
		nodeIDRaw := c.GetHeader("X-Node-ID")
		nodeID, err := strconv.ParseUint(nodeIDRaw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "bad node id"})
			return
		}
		c.Set("node_id", uint(nodeID))
		NodeHeartbeat(c)
	})

	testCases := []struct {
		nodeID         uint
		status         string
		remoteAddr     string
		expectedStatus string
		expectedAddr   string
	}{
		{nodeID: node1.ID, status: models.NodeStatusIdle, remoteAddr: "10.10.0.1:32001", expectedStatus: models.NodeStatusIdle, expectedAddr: "10.10.0.1:8002"},
		{nodeID: node2.ID, status: models.NodeStatusBusy, remoteAddr: "10.10.0.2:32002", expectedStatus: models.NodeStatusBusy, expectedAddr: "10.10.0.2:8002"},
		{nodeID: node3.ID, status: models.NodeStatusError, remoteAddr: "10.10.0.3:32003", expectedStatus: models.NodeStatusError, expectedAddr: "10.10.0.3:8002"},
	}

	var wg sync.WaitGroup
	errCh := make(chan string, len(testCases))

	for _, tc := range testCases {
		caseItem := tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"status":"%s"}`, caseItem.status)
			req := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Node-ID", fmt.Sprint(caseItem.nodeID))
			req.RemoteAddr = caseItem.remoteAddr

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				errCh <- fmt.Sprintf("node %d expected 200, got %d", caseItem.nodeID, w.Code)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for errMsg := range errCh {
		t.Fatal(errMsg)
	}

	for _, tc := range testCases {
		var node models.Node
		if err := models.DB.First(&node, tc.nodeID).Error; err != nil {
			t.Fatalf("failed to load node %d: %v", tc.nodeID, err)
		}
		if node.Status != tc.expectedStatus {
			t.Fatalf("node %d expected status %s, got %s", tc.nodeID, tc.expectedStatus, node.Status)
		}
		if node.Address != tc.expectedAddr {
			t.Fatalf("node %d expected address %s, got %s", tc.nodeID, tc.expectedAddr, node.Address)
		}
		if node.LastHeartbeatAt.IsZero() {
			t.Fatalf("node %d last_heartbeat_at should be updated", tc.nodeID)
		}
	}
}

func TestNodeHeartbeat_RejectsOfflineStatusReport(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	node := seedNodeAPIModel(t, "hb-node-offline")
	w := performNodeAPIJSONRequest(t, http.MethodPost, "/heartbeat", `{"status":"offline"}`, node.ID, NodeHeartbeat)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	resp := decodeNodeAPIResp(t, w)
	if resp["error"] != "无效的节点状态" {
		t.Fatalf("expected 无效的节点状态, got %v", resp["error"])
	}
}

func TestNodeHeartbeat_IdleReportWithActiveExamClearsNodeOccupation(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "hb-node-reboot")

	now := time.Now()
	nodeID := node.ID
	exam := models.Exam{
		Name:           "running exam",
		Subject:        "math",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      now,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"status":                   models.NodeStatusBusy,
		"current_exam_id":          exam.ID,
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
	}).Error; err != nil {
		t.Fatalf("failed to set node busy state: %v", err)
	}

	w := performNodeAPIJSONRequest(t, http.MethodPost, "/heartbeat", `{"status":"idle"}`, node.ID, NodeHeartbeat)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded models.Node
	if err := models.DB.First(&reloaded, node.ID).Error; err != nil {
		t.Fatalf("failed to reload node: %v", err)
	}
	if reloaded.Status != models.NodeStatusIdle {
		t.Fatalf("expected node status idle, got %s", reloaded.Status)
	}
	if reloaded.CurrentExamID != nil {
		t.Fatalf("expected current_exam_id to be cleared, got %v", reloaded.CurrentExamID)
	}
	if reloaded.CurrentUserID != nil {
		t.Fatalf("expected current_user_id to be cleared, got %v", reloaded.CurrentUserID)
	}
	if reloaded.CurrentUserOccupiedAt != nil {
		t.Fatalf("expected current_user_occupied_at to be cleared, got %v", reloaded.CurrentUserOccupiedAt)
	}
}

func TestNodeHeartbeat_ErrorReportClearsNodeOccupation(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "hb-node-error")

	now := time.Now()
	nodeID := node.ID
	exam := models.Exam{
		Name:           "running exam",
		Subject:        "physics",
		RoomID:         room.ID,
		NodeID:         &nodeID,
		UserID:         user.ID,
		StartTime:      now,
		ScheduleStatus: models.ExamScheduleRunning,
	}
	if err := models.DB.Create(&exam).Error; err != nil {
		t.Fatalf("failed to seed exam: %v", err)
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"status":                   models.NodeStatusBusy,
		"current_exam_id":          exam.ID,
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
	}).Error; err != nil {
		t.Fatalf("failed to set node busy state: %v", err)
	}

	w := performNodeAPIJSONRequest(t, http.MethodPost, "/heartbeat", `{"status":"error"}`, node.ID, NodeHeartbeat)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded models.Node
	if err := models.DB.First(&reloaded, node.ID).Error; err != nil {
		t.Fatalf("failed to reload node: %v", err)
	}
	if reloaded.Status != models.NodeStatusError {
		t.Fatalf("expected node status error, got %s", reloaded.Status)
	}
	if reloaded.CurrentExamID != nil {
		t.Fatalf("expected current_exam_id to be cleared, got %v", reloaded.CurrentExamID)
	}
	if reloaded.CurrentUserID != nil {
		t.Fatalf("expected current_user_id to be cleared, got %v", reloaded.CurrentUserID)
	}
	if reloaded.CurrentUserOccupiedAt != nil {
		t.Fatalf("expected current_user_occupied_at to be cleared, got %v", reloaded.CurrentUserOccupiedAt)
	}
}
