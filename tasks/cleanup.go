package tasks

import (
	"cc/models"
	"log"
	"time"
)

// StartCleanupTask 启动定时清理任务
func StartCleanupTask() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		for range ticker.C {
			cleanupStaleNodes()
			cleanupStaleExams()
		}
	}()
}

// cleanupStaleNodes 处理长时间没心跳的节点和长时间被占用的节点
func cleanupStaleNodes() {
	// 1. 如果超过 1 分钟没心跳，标记为离线并释放占用
	timeout := time.Now().Add(-1 * time.Minute)

	result := models.DB.Model(&models.Node{}).
		Where("status != ? AND last_heartbeat_at < ?", models.NodeStatusOffline, timeout).
		Updates(map[string]any{
			"status":                   models.NodeStatusOffline,
			"current_user_id":          nil, // 节点离线时释放用户占用
			"current_user_occupied_at": nil,
			"current_exam_id":          nil, // 节点离线时清除考试关联
		})

	if result.RowsAffected > 0 {
		log.Printf("[Cleanup] Marked %d stale nodes as offline and released user locks", result.RowsAffected)
	}

	// 2. 如果节点处于 idle 状态但被占用超过 2 分钟，则释放占用
	// 说明用户已经结束使用但没有显式释放，自动释放节点
	// 正在使用中的节点状态为 busy，不会被释放
	occupiedTimeout := time.Now().Add(-2 * time.Minute)

	result2 := models.DB.Model(&models.Node{}).
		Where("status = ? AND current_user_id IS NOT NULL AND current_user_occupied_at IS NOT NULL AND current_user_occupied_at < ?", models.NodeStatusIdle, occupiedTimeout).
		Updates(map[string]any{
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
		})

	if result2.RowsAffected > 0 {
		log.Printf("[Cleanup] Released %d idle nodes that were occupied >10min", result2.RowsAffected)
	}
}

// cleanupStaleExams 处理由于节点掉线或状态同步异常（如节点重启变为 idle）未正常结束的任务
func cleanupStaleExams() {
	var exams []models.Exam
	// 找到所有未结束（end_time 为 NULL）且：
	// 1. 所属节点已离线
	// 2. 所属节点返回状态为 idle（说明节点侧考试已停止，但管控中心未收到 stop 通信）
	// 3. 所属节点处于 error 状态
	err := models.DB.Joins("Node").
		Where("exams.end_time IS NULL AND (Node.status = ? OR Node.status = ? OR Node.status = ?)",
			models.NodeStatusOffline, models.NodeStatusIdle, models.NodeStatusError).
		Find(&exams).Error

	if err != nil {
		log.Printf("[Cleanup] Error finding stale exams: %v", err)
		return
	}

	for _, exam := range exams {
		// 标记结束时间为当前时间
		models.DB.Model(&exam).Update("end_time", time.Now())
		log.Printf("[Cleanup] Auto-closed stale exam %d (subject: %s) due to node status mismatch or offline", exam.ID, exam.Subject)

		// 确保节点关联也被清空
		models.DB.Model(&models.Node{}).Where("id = ?", exam.NodeID).Updates(map[string]any{
			"current_exam_id":          nil,
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
		})
	}
}
