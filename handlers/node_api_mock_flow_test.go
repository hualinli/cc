package handlers

import (
	"bytes"
	"cc/middleware"
	"cc/models"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type mockNode struct {
	baseURL            string
	token              string
	roomID             uint
	subject            string
	heartbeatInterval  time.Duration
	examineeCountSteps []int
	alertSteps         int

	client *http.Client
}

func (m *mockNode) postJSON(path string, payload any) (map[string]any, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequest(http.MethodPost, m.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Node-Token", m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return decoded, resp.StatusCode, nil
}

func (m *mockNode) runFlow() (uint, error) {
	resp, code, err := m.postJSON("/node-api/v1/tasks/sync", map[string]any{
		"action":           "start",
		"room_id":          m.roomID,
		"subject":          m.subject,
		"start_time":       time.Now().Format(time.RFC3339),
		"duration_minutes": 90,
		"examinee_count":   20,
	})
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("start failed with status %d, resp=%v", code, resp)
	}

	examIDValue, ok := resp["exam_id"].(float64)
	if !ok || uint(examIDValue) == 0 {
		return 0, fmt.Errorf("invalid exam_id in start response: %v", resp["exam_id"])
	}
	examID := uint(examIDValue)

	for i, count := range m.examineeCountSteps {
		resp, code, err = m.postJSON("/node-api/v1/tasks/sync", map[string]any{
			"action":         "sync",
			"exam_id":        examID,
			"examinee_count": count,
		})
		if err != nil {
			return 0, err
		}
		if code != http.StatusOK {
			return 0, fmt.Errorf("sync failed at step %d with status %d, resp=%v", i, code, resp)
		}

		resp, code, err = m.postJSON("/node-api/v1/alerts", map[string]any{
			"exam_id":      examID,
			"room_id":      m.roomID,
			"type":         string(models.AlertTypePhoneCheating),
			"seat_number":  fmt.Sprintf("A%d", i+1),
			"message":      "mock node alert",
			"x":            0.1,
			"y":            0.2,
		})
		if err != nil {
			return 0, err
		}
		if code != http.StatusOK {
			return 0, fmt.Errorf("alert failed at step %d with status %d, resp=%v", i, code, resp)
		}
	}

	resp, code, err = m.postJSON("/node-api/v1/tasks/sync", map[string]any{
		"action":  "stop",
		"exam_id": examID,
	})
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("stop failed with status %d, resp=%v", code, resp)
	}

	resp, code, err = m.postJSON("/node-api/v1/heartbeat", map[string]any{"status": models.NodeStatusIdle})
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("final heartbeat failed with status %d, resp=%v", code, resp)
	}

	return examID, nil
}

func (m *mockNode) run() (uint, error) {
	stopHeartbeat := make(chan struct{})
	errCh := make(chan error, 2)
	examIDCh := make(chan uint, 1)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		_, code, err := m.postJSON("/node-api/v1/heartbeat", map[string]any{"status": models.NodeStatusBusy})
		if err != nil {
			errCh <- err
			return
		}
		if code != http.StatusOK {
			errCh <- fmt.Errorf("initial heartbeat failed with status %d", code)
			return
		}

		ticker := time.NewTicker(m.heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ticker.C:
				_, hbCode, hbErr := m.postJSON("/node-api/v1/heartbeat", map[string]any{"status": models.NodeStatusBusy})
				if hbErr != nil {
					errCh <- hbErr
					return
				}
				if hbCode != http.StatusOK {
					errCh <- fmt.Errorf("periodic heartbeat failed with status %d", hbCode)
					return
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(m.heartbeatInterval + 200*time.Millisecond)
		examID, err := m.runFlow()
		if err != nil {
			errCh <- err
			close(stopHeartbeat)
			return
		}
		examIDCh <- examID
		close(stopHeartbeat)
	}()

	wg.Wait()
	close(errCh)
	close(examIDCh)

	for err := range errCh {
		if err != nil {
			return 0, err
		}
	}

	examID := uint(0)
	for v := range examIDCh {
		examID = v
	}
	if examID == 0 {
		return 0, fmt.Errorf("mock node did not produce exam_id")
	}
	return examID, nil
}

func TestMockNodeFullFlow(t *testing.T) {
	cleanup := setupNodeAPIHandlerTestDB(t)
	defer cleanup()

	user := seedNodeAPIUser(t)
	room := seedNodeAPIRoom(t)
	node := seedNodeAPIModel(t, "mock-node")
	node.Token = "mock-node-token"
	now := time.Now()
	if err := models.DB.Model(&models.Node{}).Where("id = ?", node.ID).Updates(map[string]any{
		"token":                    node.Token,
		"status":                   models.NodeStatusIdle,
		"current_user_id":          user.ID,
		"current_user_occupied_at": now,
	}).Error; err != nil {
		t.Fatalf("failed to prepare mock node: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	nodeAPI := r.Group("/node-api/v1")
	nodeAPI.Use(middleware.NodeAuthMiddleware())
	{
		nodeAPI.POST("/heartbeat", NodeHeartbeat)
		nodeAPI.POST("/tasks/sync", SyncTask)
		nodeAPI.POST("/alerts", ReportAlert)
	}

	server := httptest.NewServer(r)
	defer server.Close()

	mock := &mockNode{
		baseURL:            server.URL,
		token:              node.Token,
		roomID:             room.ID,
		subject:            "mock-subject",
		heartbeatInterval:  5 * time.Second,
		examineeCountSteps: []int{36, 40},
		alertSteps:         2,
		client:             &http.Client{Timeout: 3 * time.Second},
	}

	examID, err := mock.run()
	if err != nil {
		t.Fatalf("mock node run failed: %v", err)
	}

	var exam models.Exam
	if err := models.DB.First(&exam, examID).Error; err != nil {
		t.Fatalf("failed to load exam: %v", err)
	}
	if exam.EndTime == nil {
		t.Fatal("expected exam to be ended by stop action")
	}
	if exam.ExamineeCount != 40 {
		t.Fatalf("expected examinee_count 40, got %d", exam.ExamineeCount)
	}

	var alertsCount int64
	if err := models.DB.Model(&models.Alert{}).Where("exam_id = ?", examID).Count(&alertsCount).Error; err != nil {
		t.Fatalf("failed to count alerts: %v", err)
	}
	if alertsCount < 2 {
		t.Fatalf("expected at least 2 alerts, got %d", alertsCount)
	}

	var reloadedNode models.Node
	if err := models.DB.First(&reloadedNode, node.ID).Error; err != nil {
		t.Fatalf("failed to reload node: %v", err)
	}
	if reloadedNode.Status != models.NodeStatusIdle {
		t.Fatalf("expected node status idle, got %s", reloadedNode.Status)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Fatalf("expected node current_exam_id nil, got %v", reloadedNode.CurrentExamID)
	}
	if reloadedNode.CurrentUserID != nil {
		t.Fatalf("expected node current_user_id nil, got %v", reloadedNode.CurrentUserID)
	}
}


// TODO: 节点异常流程测试，如心跳停止等。