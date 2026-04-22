package tasks

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const examSchedulerInterval = 15 * time.Second

var (
	errNoAvailableNode = errors.New("no available node")
	errInvalidDuration = errors.New("duration_seconds must be greater than 0")
	scheduleMutex      sync.Mutex
)

// StartExamScheduler 启动自动开考调度任务。
func StartExamScheduler() {
	go func() {
		ticker := time.NewTicker(examSchedulerInterval)
		defer ticker.Stop()

		for range ticker.C {
			processDueExams()
		}
	}()
}

func processDueExams() {
	now := time.Now()
	var dueExam models.Exam
	result := models.DB.Where("start_time <= ? AND end_time IS NULL AND schedule_status IN ?", now,
		[]string{models.ExamSchedulePending, models.ExamScheduleAssigned}).
		Order("start_time asc, created_at asc, id asc").
		Limit(1).
		Find(&dueExam)
	if result.Error != nil {
		log.Printf("[ExamScheduler] query due exams failed: %v", result.Error)
		return
	}
	if result.RowsAffected == 0 {
		return
	}

	if err := scheduleExamByID(dueExam.ID, false); err != nil {
		log.Printf("[ExamScheduler] auto schedule exam=%d failed: %v", dueExam.ID, err)
	}
}

// RetryScheduleExam 手动重试单场考试的分配与通知。
func RetryScheduleExam(examID uint) error {
	return scheduleExamByID(examID, true)
}

func scheduleExamByID(examID uint, manualRetry bool) error {
	scheduleMutex.Lock()
	defer scheduleMutex.Unlock()
	_ = manualRetry

	var exam models.Exam
	var node models.Node
	needNotify := false
	lockedNodeInTx := false

	txErr := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&exam, examID).Error; err != nil {
			return err
		}

		if exam.EndTime != nil {
			return nil
		}
		if exam.StartTime.After(time.Now()) {
			return nil
		}
		if exam.ScheduleStatus == models.ExamScheduleRunning {
			return nil
		}
		if exam.DurationSeconds <= 0 {
			return errInvalidDuration
		}

		if exam.NodeID == nil {
			lockedNode, found, lockErr := lockAvailableNodeForExam(tx, exam)
			if lockErr != nil {
				return lockErr
			}
			if !found {
				return errNoAvailableNode
			}
			node = lockedNode
			if err := tx.Model(&exam).Updates(map[string]any{
				"node_id":         node.ID,
				"schedule_status": models.ExamScheduleAssigned,
				"schedule_error":  "",
				"updated_at":      time.Now(),
			}).Error; err != nil {
				return err
			}
			exam.NodeID = &node.ID
			exam.ScheduleStatus = models.ExamScheduleAssigned
			lockedNodeInTx = true
		} else {
			if err := tx.First(&node, *exam.NodeID).Error; err != nil {
				return err
			}

			if node.Status == models.NodeStatusOffline {
				return fmt.Errorf("node %d is offline", node.ID)
			}
			if node.LastHeartbeatAt.Before(time.Now().Add(-1 * time.Minute)) {
				return fmt.Errorf("node %d heartbeat expired", node.ID)
			}

			// 防止同一节点存在其他进行中的考试。
			var conflictCount int64
			if err := tx.Model(&models.Exam{}).
				Where("node_id = ? AND end_time IS NULL AND id <> ?", node.ID, exam.ID).
				Count(&conflictCount).Error; err != nil {
				return err
			}
			if conflictCount > 0 {
				return fmt.Errorf("node %d has another active exam", node.ID)
			}

			now := time.Now()
			lockResult := tx.Model(&models.Node{}).
				Where("id = ? AND status <> ? AND last_heartbeat_at >= ? AND (current_exam_id IS NULL OR current_exam_id = ?)", node.ID, models.NodeStatusOffline, time.Now().Add(-1*time.Minute), exam.ID).
				Updates(map[string]any{
					"status":                   models.NodeStatusBusy,
					"current_user_id":          exam.UserID,
					"current_user_occupied_at": now,
					"current_exam_id":          exam.ID,
				})
			if lockResult.Error != nil {
				return lockResult.Error
			}
			if lockResult.RowsAffected == 0 {
				return fmt.Errorf("node %d is occupied by another exam", node.ID)
			}

			if err := tx.Model(&exam).Updates(map[string]any{
				"schedule_status": models.ExamScheduleAssigned,
				"schedule_error":  "",
				"updated_at":      now,
			}).Error; err != nil {
				return err
			}
			exam.ScheduleStatus = models.ExamScheduleAssigned
			lockedNodeInTx = true
		}

		if exam.NodeID == nil {
			return errors.New("node assignment missing")
		}

		if node.ID == 0 {
			if err := tx.First(&node, *exam.NodeID).Error; err != nil {
				return err
			}
		}
		needNotify = true
		return nil
	})
	if txErr != nil {
		statusUpdates := map[string]any{}
		switch {
		case errors.Is(txErr, errNoAvailableNode):
			statusUpdates["schedule_status"] = models.ExamScheduleAssignFail
			statusUpdates["schedule_error"] = txErr.Error()
		case errors.Is(txErr, errInvalidDuration):
			statusUpdates["schedule_status"] = models.ExamScheduleNotifyFail
			statusUpdates["schedule_error"] = txErr.Error()
			statusUpdates["node_id"] = nil
		case strings.Contains(strings.ToLower(txErr.Error()), "unique constraint failed: exams.node_id"):
			statusUpdates["schedule_status"] = models.ExamScheduleAssignFail
			statusUpdates["schedule_error"] = txErr.Error()
			statusUpdates["node_id"] = nil
		}
		if len(statusUpdates) > 0 {
			if dbErr := models.DB.Model(&models.Exam{}).Where("id = ?", examID).Updates(statusUpdates).Error; dbErr != nil {
				log.Printf("[ExamScheduler] persist failure status failed exam=%d: %v", examID, dbErr)
			}
		}
		return txErr
	}

	if !needNotify {
		return nil
	}

	if err := notifyNodeStartExam(node, exam); err != nil {
		rollbackUpdates := map[string]any{
			"schedule_status": models.ExamScheduleNotifyFail,
			"schedule_error":  err.Error(),
			"node_id":         nil,
		}
		if dbErr := models.DB.Model(&models.Exam{}).Where("id = ?", exam.ID).Updates(rollbackUpdates).Error; dbErr != nil {
			log.Printf("[ExamScheduler] update notify failure status failed exam=%d: %v", exam.ID, dbErr)
		}
		if lockedNodeInTx {
			unlockNodeForExam(node.ID, exam.ID, "rollback")
		}
		return err
	}

	updateRunning := models.DB.Model(&models.Exam{}).Where("id = ? AND end_time IS NULL", exam.ID).Updates(map[string]any{
		"schedule_status": models.ExamScheduleRunning,
		"schedule_error":  "",
	})
	if updateRunning.Error != nil {
		return updateRunning.Error
	}
	if updateRunning.RowsAffected == 0 {
		log.Printf("[ExamScheduler] skip set running because exam=%d already ended", exam.ID)
		if lockedNodeInTx {
			unlockNodeForExam(node.ID, exam.ID, "ended-before-running")
		}
	}

	return nil
}

func unlockNodeForExam(nodeID, examID uint, reason string) {
	if dbErr := models.DB.Model(&models.Node{}).
		Where("id = ? AND status = ? AND current_exam_id = ?", nodeID, models.NodeStatusBusy, examID).
		Updates(map[string]any{
			"status":                   models.NodeStatusIdle,
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
			"current_exam_id":          nil,
		}).Error; dbErr != nil {
		log.Printf("[ExamScheduler] unlock node failed node=%d exam=%d reason=%s: %v", nodeID, examID, reason, dbErr)
	}
	if dbErr := models.DB.Model(&models.Node{}).
		Where("id = ? AND status <> ? AND current_exam_id = ?", nodeID, models.NodeStatusBusy, examID).
		Updates(map[string]any{
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
			"current_exam_id":          nil,
		}).Error; dbErr != nil {
		log.Printf("[ExamScheduler] unlock node exam cleanup failed node=%d exam=%d reason=%s: %v", nodeID, examID, reason, dbErr)
	}
}

func lockAvailableNodeForExam(tx *gorm.DB, exam models.Exam) (models.Node, bool, error) {
	for i := 0; i < 5; i++ {
		candidate, found, err := pickAvailableNode(tx)
		if err != nil {
			return models.Node{}, false, err
		}
		if !found {
			return models.Node{}, false, nil
		}

		now := time.Now()
		lockResult := tx.Model(&models.Node{}).
			Where("id = ? AND status = ? AND current_user_id IS NULL AND current_exam_id IS NULL AND last_heartbeat_at >= ?", candidate.ID, models.NodeStatusIdle, now.Add(-1*time.Minute)).
			Updates(map[string]any{
				"status":                   models.NodeStatusBusy,
				"current_user_id":          exam.UserID,
				"current_user_occupied_at": now,
				"current_exam_id":          exam.ID,
			})
		if lockResult.Error != nil {
			return models.Node{}, false, lockResult.Error
		}
		if lockResult.RowsAffected > 0 {
			candidate.Status = models.NodeStatusBusy
			candidate.CurrentUserID = &exam.UserID
			candidate.CurrentExamID = &exam.ID
			candidate.CurrentUserOccupiedAt = &now
			return candidate, true, nil
		}
	}

	return models.Node{}, false, nil
}

func pickAvailableNode(tx *gorm.DB) (models.Node, bool, error) {
	timeout := time.Now().Add(-1 * time.Minute)
	var node models.Node
	result := tx.Where("status = ? AND current_user_id IS NULL AND current_exam_id IS NULL AND last_heartbeat_at >= ?", models.NodeStatusIdle, timeout).
		Order("id asc").
		Limit(1).
		Find(&node)
	if result.Error != nil {
		return models.Node{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		return models.Node{}, false, nil
	}
	return node, true, nil
}

func notifyNodeStartExam(node models.Node, exam models.Exam) error {
	if strings.TrimSpace(node.Address) == "" || node.Address == "waiting_for_heartbeat" {
		return errors.New("node address unavailable")
	}
	if strings.TrimSpace(node.Token) == "" {
		return errors.New("node token not configured")
	}

	body := map[string]any{
		"subject":      exam.Subject,
		"duration":     strconv.Itoa((exam.DurationSeconds + 59) / 60),
		"classroom_id": exam.RoomID,
		"exam_id":      exam.ID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	startURL := fmt.Sprintf("http://%s/exam/schedule_start", node.Address)
	parsedURL, err := url.Parse(startURL)
	if err != nil {
		return err
	}
	query := parsedURL.Query()
	query.Set("token", node.Token)
	parsedURL.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodPost, parsedURL.String(), bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		respText := strings.TrimSpace(string(respBytes))
		if respText == "" {
			return fmt.Errorf("node notify failed with status %d", resp.StatusCode)
		}
		return fmt.Errorf("node notify failed with status %d: %s", resp.StatusCode, respText)
	}

	var resBody struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resBody); err == nil {
		if !resBody.Success {
			if strings.TrimSpace(resBody.Error) != "" {
				return errors.New(resBody.Error)
			}
			return errors.New("node returned success=false")
		}
	}

	log.Printf("[ExamScheduler] notified node=%d exam=%d start", node.ID, exam.ID)
	return nil
}
