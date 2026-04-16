package tasks

import (
	"cc/models"
	"log"
	"time"

	"gorm.io/gorm"
)

func StartCleanupTask() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cleanupStaleNodes()
			cleanupStaleExams()
		}
	}()
}

func cleanupStaleNodes() {
	// 1. 正常离线：idle 节点超过心跳超时时间未发心跳时，标记为 offline。
	//    这里同样适用于 busy/error 等非 offline 状态的节点。
	//    idle 节点离线时要释放当前用户占用并清除考试缓存字段。
	// 3. 运行中节点掉线（busy 且无心跳）也会在这里被置为 offline，
	//    随后由 cleanupStaleExams 负责结束关联考试并写入自动终止原因。
	timeout := time.Now().Add(-1 * time.Minute)

	result := models.DB.Model(&models.Node{}).
		Where("status != ? AND last_heartbeat_at < ?", models.NodeStatusOffline, timeout).
		Updates(map[string]any{
			"status":                   models.NodeStatusOffline,
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
			"current_exam_id":          nil,
		})
	if result.Error != nil {
		log.Printf("[Cleanup] failed to mark stale nodes offline: %v", result.Error)
		return
	}

	if result.RowsAffected > 0 {
		log.Printf("[Cleanup] Marked %d stale nodes as offline and released user locks", result.RowsAffected)
	}

	occupiedTimeout := time.Now().Add(-2 * time.Minute)

	// 2. 如果节点长时间被占用但没有真正开始考试，释放占用并恢复到 idle。
	//    这包括 idle 节点和 busy 状态下 current_exam_id 仍为 NULL 的情况。
	result2 := models.DB.Model(&models.Node{}).
		Where("status IN (?, ?) AND current_exam_id IS NULL AND current_user_id IS NOT NULL AND current_user_occupied_at IS NOT NULL AND current_user_occupied_at < ?", models.NodeStatusIdle, models.NodeStatusBusy, occupiedTimeout).
		Updates(map[string]any{
			"status":                   models.NodeStatusIdle,
			"current_user_id":          nil,
			"current_user_occupied_at": nil,
		})
	if result2.Error != nil {
		log.Printf("[Cleanup] failed to release stale idle node occupation: %v", result2.Error)
		return
	}

	if result2.RowsAffected > 0 {
		log.Printf("[Cleanup] Released %d idle nodes that were occupied >2min", result2.RowsAffected)
	}
}

func cleanupStaleExams() {
	var exams []models.Exam
	// 覆盖四类异常场景：
	// 1) 节点 offline / error / idle
	// 2) exam.node_id 为空（历史脏数据或异常写入）
	// 3) exam.node_id 存在但节点记录缺失
	// 对运行中且未结束的考试做自动终止收敛，避免僵尸考试长期残留。
	err := models.DB.Table("exams").
		Select("exams.*").
		Joins("LEFT JOIN nodes ON nodes.id = exams.node_id").
		Where("exams.end_time IS NULL AND exams.schedule_status = ? AND (exams.node_id IS NULL OR nodes.id IS NULL OR nodes.status = ? OR nodes.status = ? OR nodes.status = ?)",
			models.ExamScheduleRunning, models.NodeStatusOffline, models.NodeStatusError, models.NodeStatusIdle).
		Find(&exams).Error

	if err != nil {
		log.Printf("[Cleanup] Error finding stale exams: %v", err)
		return
	}

	for _, exam := range exams {
		err := models.DB.Transaction(func(tx *gorm.DB) error {
			autoStopReason := "节点状态异常，自动终止"
			if exam.NodeID == nil {
				autoStopReason = "由于节点关联缺失自动终止"
			}
			if exam.NodeID != nil {
				var node models.Node
				nodeLookupErr := tx.Select("status").First(&node, *exam.NodeID).Error
				if nodeLookupErr == nil {
					switch node.Status {
					case models.NodeStatusOffline:
						autoStopReason = "由于节点掉线自动终止"
					case models.NodeStatusError:
						autoStopReason = "由于节点异常自动终止"
					case models.NodeStatusIdle:
						autoStopReason = "由于节点状态空闲自动终止"
					}
				} else if nodeLookupErr == gorm.ErrRecordNotFound {
					autoStopReason = "由于节点记录缺失自动终止"
				}
			}

			now := time.Now()
			updateResult := tx.Model(&models.Exam{}).
				Where("id = ? AND end_time IS NULL", exam.ID).
				Updates(map[string]any{
					"end_time":       now,
					"schedule_error": autoStopReason,
					"updated_at":     now,
				})
			if updateResult.Error != nil {
				return updateResult.Error
			}
			if updateResult.RowsAffected == 0 {
				return nil
			}

			if exam.NodeID != nil {
				nodeResult := tx.Model(&models.Node{}).
					Where("id = ? AND current_exam_id = ?", *exam.NodeID, exam.ID).
					Updates(map[string]any{
						"current_exam_id":          nil,
						"current_user_id":          nil,
						"current_user_occupied_at": nil,
					})
				if nodeResult.Error != nil {
					return nodeResult.Error
				}
			}

			return nil
		})

		if err != nil {
			log.Printf("[Cleanup] failed to auto-close stale exam %d: %v", exam.ID, err)
			continue
		}

		log.Printf("[Cleanup] Auto-closed stale running exam %d (subject: %s) due to node offline/error", exam.ID, exam.Subject)
	}
}
