package tasks

import (
	"cc/models"
	"log"
	"time"

	"gorm.io/gorm"
)

const cleanupInterval = 1 * time.Minute

// startupGracePeriod 启动后的宽限期，避免因控制中心重启导致所有节点租约过期而被误关考试。
const startupGracePeriod = 2 * time.Minute

var cleanupStartedAt = time.Now()

// StartCleanupTask 启动定期清理任务
func StartCleanupTask() {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		log.Println("[Cleanup] Task started, interval:", cleanupInterval)
		log.Printf("[Cleanup] Startup grace period: %v (lease checks deferred)", startupGracePeriod)

		for range ticker.C {
			runCleanupTasks()
		}
	}()
}

func runCleanupTasks() {
	// 1. 检查租约过期的节点
	checkExpiredLeases()

	// 2. 关闭超时的考试
	closeExpiredExams()

	// 3. 清理孤儿数据
	cleanOrphanData()

	// 4. 标记离线节点（基于心跳超时）
	markOfflineNodes()
}

// checkExpiredLeases 检查租约过期的节点，自动关闭其考试。
//
// 兜底机制：
//   - 节点断电：心跳停止 → lease_expires_at 过期 → 关闭考试 + 标记 interrupted
//   - 控制中心重启：启动后有 2 分钟宽限期，允许节点重新发送心跳刷新租约，
//     避免将所有运行中的考试误关。
//   - 双重条件：除 lease_expires_at 外，额外检查 last_heartbeat_at，
//     确保节点确实停止通信（而非控制中心单方面重启）。
func checkExpiredLeases() {
	now := time.Now()

	// 启动宽限期：控制中心重启后等待节点重新心跳，避免误关考试
	if time.Since(cleanupStartedAt) < startupGracePeriod {
		return
	}

	// 双重条件：租约过期 + 心跳超时（节点确实失联）
	heartbeatTimeout := now.Add(-2 * time.Minute)
	var expiredNodes []models.Node
	result := models.DB.Where(
		"lease_expires_at < ? AND last_heartbeat_at < ? AND current_exam_id IS NOT NULL",
		now, heartbeatTimeout,
	).Find(&expiredNodes)

	if result.Error != nil {
		log.Printf("[Cleanup] query expired leases failed: %v", result.Error)
		return
	}

	if len(expiredNodes) == 0 {
		return
	}

	log.Printf("[Cleanup] found %d nodes with expired leases (lease expired + heartbeat timeout)", len(expiredNodes))

	for _, node := range expiredNodes {
		if node.CurrentExamID == nil {
			continue
		}

		examID := *node.CurrentExamID

		// 在事务中处理
		err := models.DB.Transaction(func(tx *gorm.DB) error {
			// 1. 检查考试是否仍在进行中
			var exam models.Exam
			if err := tx.Where("id = ? AND end_time IS NULL", examID).First(&exam).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					// 考试已结束，只需清理节点
					return nil
				}
				return err
			}

			// 2. 关闭考试
			if err := tx.Model(&models.Exam{}).
				Where("id = ?", examID).
				Updates(map[string]any{
					"end_time":        now,
					"schedule_status": models.ExamScheduleInterrupted, // 标记为中断
				}).Error; err != nil {
				return err
			}

			log.Printf("[Cleanup] closed exam %d due to node %d lease expired", examID, node.ID)

			// 3. 创建告警
			alert := models.Alert{
				ExamID:  examID,
				Type:    "node_lease_expired",
				Message: "节点租约过期，考试已自动关闭",
			}
			if err := tx.Create(&alert).Error; err != nil {
				log.Printf("[Cleanup] create alert failed: %v", err)
				// 不阻止事务继续
			}

			return nil
		})

		if err != nil {
			log.Printf("[Cleanup] handle expired lease for node %d failed: %v", node.ID, err)
			continue
		}

		// 4. 清空节点占用并标记离线
		if err := models.DB.Model(&models.Node{}).
			Where("id = ?", node.ID).
			Updates(map[string]any{
				"current_exam_id": nil,
				"status":          models.NodeStatusOffline,
			}).Error; err != nil {
			log.Printf("[Cleanup] clear node %d occupation failed: %v", node.ID, err)
		}
	}
}

// closeExpiredExams 关闭超时的考试（超过预期结束时间 + 宽限期）
func closeExpiredExams() {
	now := time.Now()
	gracePeriod := 10 * time.Minute // 宽限期 10 分钟

	var expiredExams []models.Exam
	result := models.DB.Where("end_time IS NULL AND duration_seconds > 0").
		Find(&expiredExams)

	if result.Error != nil {
		log.Printf("[Cleanup] query expired exams failed: %v", result.Error)
		return
	}

	closedCount := 0
	for _, exam := range expiredExams {
		expectedEnd := exam.StartTime.Add(time.Duration(exam.DurationSeconds) * time.Second)
		deadline := expectedEnd.Add(gracePeriod)

		if now.After(deadline) {
			// 超时，自动关闭
			err := models.DB.Transaction(func(tx *gorm.DB) error {
				// 关闭考试
				if err := tx.Model(&models.Exam{}).
					Where("id = ? AND end_time IS NULL", exam.ID).
					Updates(map[string]any{
						"end_time":        now,
						"schedule_status": models.ExamScheduleAutoClosed, // 标记为自动关闭
					}).Error; err != nil {
					return err
				}

				// 释放节点
				if exam.NodeID != nil {
					if err := tx.Model(&models.Node{}).
						Where("id = ? AND current_exam_id = ?", *exam.NodeID, exam.ID).
						Updates(map[string]any{
							"current_exam_id": nil,
							"status":          models.NodeStatusIdle,
						}).Error; err != nil {
						log.Printf("[Cleanup] release node %d failed: %v", *exam.NodeID, err)
					}
				}

				// 创建告警
				alert := models.Alert{
					ExamID:  exam.ID,
					Type:    "exam_auto_closed",
					Message: "考试超时，已自动关闭",
				}
				if err := tx.Create(&alert).Error; err != nil {
					log.Printf("[Cleanup] create alert failed: %v", err)
				}

				return nil
			})

			if err != nil {
				log.Printf("[Cleanup] close expired exam %d failed: %v", exam.ID, err)
			} else {
				closedCount++
			}
		}
	}

	if closedCount > 0 {
		log.Printf("[Cleanup] auto closed %d expired exams", closedCount)
	}
}

// cleanOrphanData 清理孤儿数据
func cleanOrphanData() {
	// 1. 清理空闲但有 current_exam_id 的节点
	result := models.DB.Model(&models.Node{}).
		Where("status = ? AND current_exam_id IS NOT NULL", models.NodeStatusIdle).
		Update("current_exam_id", nil)

	if result.Error != nil {
		log.Printf("[Cleanup] clean orphan current_exam_id failed: %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Printf("[Cleanup] cleaned %d orphan current_exam_id", result.RowsAffected)
	}

	// 2. 清理指向已结束考试的 current_exam_id
	var nodes []models.Node
	models.DB.Where("current_exam_id IS NOT NULL").Find(&nodes)

	for _, node := range nodes {
		if node.CurrentExamID == nil {
			continue
		}

		var exam models.Exam
		err := models.DB.Where("id = ?", *node.CurrentExamID).First(&exam).Error

		shouldClear := false
		if err == gorm.ErrRecordNotFound {
			// 考试不存在
			shouldClear = true
		} else if err == nil && exam.EndTime != nil {
			// 考试已结束
			shouldClear = true
		}

		if shouldClear {
			if err := models.DB.Model(&models.Node{}).
				Where("id = ?", node.ID).
				Update("current_exam_id", nil).Error; err != nil {
				log.Printf("[Cleanup] clear node %d current_exam_id failed: %v", node.ID, err)
			} else {
				log.Printf("[Cleanup] cleared node %d pointing to ended/missing exam", node.ID)
			}
		}
	}
}

// markOfflineNodes 标记心跳超时的节点为离线（基于 last_heartbeat_at）
func markOfflineNodes() {
	timeout := time.Now().Add(-5 * time.Minute) // 5 分钟未心跳

	result := models.DB.Model(&models.Node{}).
		Where("status <> ? AND last_heartbeat_at < ?", models.NodeStatusOffline, timeout).
		Update("status", models.NodeStatusOffline)

	if result.Error != nil {
		log.Printf("[Cleanup] mark offline nodes failed: %v", result.Error)
	} else if result.RowsAffected > 0 {
		log.Printf("[Cleanup] marked %d nodes as offline", result.RowsAffected)
	}
}
