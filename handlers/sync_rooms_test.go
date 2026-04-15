package handlers

import (
	"cc/models"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func performSyncRoomsRequest(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/sync/rooms", SyncRooms)

	req := httptest.NewRequest(http.MethodPost, "/sync/rooms", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedSyncNode(t *testing.T, name string, address string, token string) {
	t.Helper()

	node := models.Node{
		Name:      name,
		Token:     token,
		Address:   address,
		Status:    models.NodeStatusIdle,
		NodeModel: "model-x",
		Version:   "1.0.0",
	}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
}

func TestSyncRoomsSuccess(t *testing.T) {
	cleanup := setupRoomsHandlerTestDB(t)
	defer cleanup()

	// 按要求通过 create API 创建多条可重复教室数据
	w1 := performCreateRoomRequest(t, `{"name":"A101","building":"Main","rtsp_url":"rtsp://cam/1"}`)
	if w1.Code != http.StatusOK {
		t.Fatalf("failed to create first room, status=%d, body=%s", w1.Code, w1.Body.String())
	}
	w2 := performCreateRoomRequest(t, `{"name":"A101","building":"Main","rtsp_url":"rtsp://cam/1"}`)
	if w2.Code != http.StatusOK {
		t.Fatalf("failed to create second room, status=%d, body=%s", w2.Code, w2.Body.String())
	}

	var requestCount int32
	mockNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/classrooms" {
			t.Fatalf("expected /classrooms, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("token") != "token-1" {
			t.Fatalf("expected token token-1, got %s", r.URL.Query().Get("token"))
		}

		var payload struct {
			Version    string `json:"version"`
			Classrooms []struct {
				ID       uint   `json:"id"`
				Building string `json:"building"`
				Name     string `json:"name"`
				URL      string `json:"url"`
			} `json:"classrooms"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode payload: %v", err)
		}
		if payload.Version != "1.0" {
			t.Fatalf("expected version 1.0, got %s", payload.Version)
		}
		if len(payload.Classrooms) != 2 {
			t.Fatalf("expected 2 classrooms, got %d", len(payload.Classrooms))
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer mockNode.Close()

	seedSyncNode(t, "node-1", mockNode.URL, "token-1")

	w := performSyncRoomsRequest(t)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got false, body=%s", w.Body.String())
	}
	if !strings.Contains(resp.Message, "所有节点同步成功") {
		t.Fatalf("expected success message, got %s", resp.Message)
	}
	if atomic.LoadInt32(&requestCount) != 1 {
		t.Fatalf("expected 1 outbound request, got %d", atomic.LoadInt32(&requestCount))
	}
}

func TestSyncRoomsPartialFailure(t *testing.T) {
	cleanup := setupRoomsHandlerTestDB(t)
	defer cleanup()

	wCreate := performCreateRoomRequest(t, `{"name":"A101","building":"Main","rtsp_url":"rtsp://cam/1"}`)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("failed to create room, status=%d, body=%s", wCreate.Code, wCreate.Body.String())
	}

	okNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer okNode.Close()

	failNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer failNode.Close()

	seedSyncNode(t, "node-ok", okNode.URL, "token-ok")
	seedSyncNode(t, "node-fail", failNode.URL, "token-fail")

	w := performSyncRoomsRequest(t)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false for partial failure, got true")
	}
	if !strings.Contains(resp.Message, "部分节点同步失败") {
		t.Fatalf("expected partial failure message, got %s", resp.Message)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 failure item, got %d, errors=%v", len(resp.Errors), resp.Errors)
	}
	if !strings.Contains(resp.Errors[0], "node-fail") {
		t.Fatalf("expected failure to mention node-fail, got %s", resp.Errors[0])
	}
}

func TestSyncRoomsConnectionError(t *testing.T) {
	cleanup := setupRoomsHandlerTestDB(t)
	defer cleanup()

	wCreate := performCreateRoomRequest(t, `{"name":"A101","building":"Main","rtsp_url":"rtsp://cam/1"}`)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("failed to create room, status=%d, body=%s", wCreate.Code, wCreate.Body.String())
	}

	// 127.0.0.1:1 通常无服务监听，可稳定触发连接错误分支。
	seedSyncNode(t, "node-conn-err", "127.0.0.1:1", "token-conn-err")

	w := performSyncRoomsRequest(t)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Success {
		t.Fatalf("expected success=false for connection error, got true")
	}
	if !strings.Contains(resp.Message, "部分节点同步失败") {
		t.Fatalf("expected partial failure message, got %s", resp.Message)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("expected exactly 1 failure item, got %d, errors=%v", len(resp.Errors), resp.Errors)
	}
	if !strings.Contains(resp.Errors[0], "node-conn-err") {
		t.Fatalf("expected failure to mention node-conn-err, got %s", resp.Errors[0])
	}
	if !strings.Contains(strings.ToLower(resp.Errors[0]), "connect") && !strings.Contains(strings.ToLower(resp.Errors[0]), "refused") {
		t.Fatalf("expected connection error detail, got %s", resp.Errors[0])
	}
}
