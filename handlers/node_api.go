package handlers

import (
	"cc/models"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NodeHeartbeat 处理节点心跳
func NodeHeartbeat(c *gin.Context) {
	nodeID, _ := c.Get("node_id")

	var input struct {
		Status  string         `json:"status"`
		Details map[string]any `json:"details"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误",
		})
		return
	}

	if input.Status != "" {
		switch input.Status {
		case models.NodeStatusIdle, models.NodeStatusBusy, models.NodeStatusError, models.NodeStatusOffline:
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "无效的节点状态",
			})
			return
		}
	}

	// 查询当前节点状态
	var node models.Node
	if err := models.DB.Where("id = ?", nodeID).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 更新数据库状态
	reportedAddress := c.ClientIP() + ":8002"
	updateData := map[string]any{
		"last_heartbeat_at": time.Now(),
	}
	if node.Address != reportedAddress {
		updateData["address"] = reportedAddress // 心跳时按需更新节点地址，减少无效写入
	}

	// 心跳时更新节点的真实状态（idle/busy/error）
	// 但如果节点离线则不更新（由清理任务处理离线状态）
	// 另外：若节点上报 idle 且 current_exam_id 不为空，先校验是否仍有未结束考试；
	// - 若存在活动考试：忽略 idle 回报，避免误降级导致考试被错误清理。
	// - 若不存在活动考试：自动清理残留关联并恢复 idle（自愈）。
	if input.Status != "" && input.Status != models.NodeStatusOffline {
		if input.Status == models.NodeStatusIdle && node.CurrentExamID != nil {
			var activeExamCount int64
			countErr := models.DB.Model(&models.Exam{}).
				Where("id = ? AND node_id = ? AND end_time IS NULL", *node.CurrentExamID, node.ID).
				Count(&activeExamCount).Error
			if countErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"error":   "查询活动考试失败",
				})
				return
			}
			if activeExamCount == 0 {
				if node.Status != models.NodeStatusIdle {
					updateData["status"] = models.NodeStatusIdle
				}
				updateData["current_exam_id"] = nil
				updateData["current_user_id"] = nil
				updateData["current_user_occupied_at"] = nil
			}
		} else {
			if node.Status != input.Status {
				updateData["status"] = input.Status
			}
		}
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", nodeID).UpdateColumns(updateData).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新节点状态失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// SyncTask 同步考试状态
func SyncTask(c *gin.Context) {
	nodeID, _ := c.Get("node_id")
	nodeIDUint, ok := nodeID.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "内部错误：节点ID类型异常",
		})
		return
	}

	var input struct {
		Action          string    `json:"action"` // start, stop, sync
		RoomID          uint      `json:"room_id"`
		Subject         string    `json:"subject"`
		StartTime       time.Time `json:"start_time"`
		DurationMinutes int       `json:"duration_minutes"` // 考试时长（分钟）
		ExamID          uint      `json:"exam_id"`
		ExamineeCount   int       `json:"examinee_count"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "请求参数错误: " + err.Error(),
		})
		return
	}

	// 获取节点信息
	var node models.Node
	if err := models.DB.First(&node, nodeIDUint).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	switch input.Action {
	case "start":
		// 开始考试
		if input.RoomID == 0 || input.Subject == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "缺少必要参数: room_id 或 subject",
			})
			return
		}

		// 检查 Room 是否存在
		var room models.Room
		if err := models.DB.First(&room, input.RoomID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   fmt.Sprintf("Room ID %d 不存在", input.RoomID),
			})
			return
		}

		var responseExamID uint
		txErr := models.DB.Transaction(func(tx *gorm.DB) error {
			var currentNode models.Node
			if err := tx.First(&currentNode, nodeIDUint).Error; err != nil {
				return err
			}

			// 控制中心已分配考试ID时，优先走幂等确认，避免 current_user_id 短暂丢失导致 start 失败。
			if input.ExamID != 0 {
				var assignedExam models.Exam
				assignedResult := tx.Where("id = ?", input.ExamID).Limit(1).Find(&assignedExam)
				if assignedResult.Error != nil {
					return assignedResult.Error
				}
				if assignedResult.RowsAffected == 0 {
					return fmt.Errorf("exam_id 无效")
				}
				if assignedExam.NodeID == nil || *assignedExam.NodeID != nodeIDUint {
					return fmt.Errorf("exam_id 不属于当前节点")
				}
				if assignedExam.EndTime != nil {
					return fmt.Errorf("考试已结束")
				}
				if assignedExam.RoomID != input.RoomID {
					return fmt.Errorf("room_id 与考试不匹配")
				}

				now := time.Now()
				if err := tx.Model(&currentNode).Updates(map[string]any{
					"current_exam_id":          assignedExam.ID,
					"current_user_id":          assignedExam.UserID,
					"current_user_occupied_at": now,
					"status":                   models.NodeStatusBusy,
				}).Error; err != nil {
					return err
				}

				responseExamID = assignedExam.ID
				return nil
			}

			// 节点未被占用时不允许节点自行开考。
			if currentNode.CurrentUserID == nil {
				return fmt.Errorf("节点未被任何监考员占用")
			}

			// 幂等保障：同一节点若已存在未结束考试，直接复用并返回 exam_id。
			var activeExam models.Exam
			activeExamResult := tx.Where("node_id = ? AND end_time IS NULL", nodeIDUint).
				Order("created_at asc, id asc").
				Limit(1).
				Find(&activeExam)
			if activeExamResult.Error != nil {
				return activeExamResult.Error
			}
			if activeExamResult.RowsAffected > 0 {
				if err := tx.Model(&currentNode).Updates(map[string]any{
					"current_exam_id": activeExam.ID,
					"status":          models.NodeStatusBusy,
				}).Error; err != nil {
					return err
				}
				responseExamID = activeExam.ID
				return nil
			}

			nodeIDPtr := nodeIDUint
			exam := models.Exam{
				Name:            input.Subject + "考试",
				Subject:         input.Subject,
				RoomID:          input.RoomID,
				NodeID:          &nodeIDPtr,
				UserID:          *currentNode.CurrentUserID,
				DurationSeconds: input.DurationMinutes * 60,
				StartTime:       input.StartTime,
				EndTime:         nil,
				ScheduleStatus:  models.ExamScheduleRunning,
				ExamineeCount:   input.ExamineeCount,
			}

			if err := tx.Create(&exam).Error; err != nil {
				return err
			}

			if err := tx.Model(&currentNode).Updates(map[string]any{
				"current_exam_id": exam.ID,
				"status":          models.NodeStatusBusy,
			}).Error; err != nil {
				return err
			}

			responseExamID = exam.ID
			return nil
		})
		if txErr != nil {
			status := http.StatusInternalServerError
			if txErr.Error() == "节点未被任何监考员占用" {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{
				"success": false,
				"error":   txErr.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "exam_id": responseExamID})

	case "stop":
		// 结束考试
		if input.ExamID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "缺少必要参数: exam_id",
			})
			return
		}

		var exam models.Exam
		result := models.DB.Where("id = ?", input.ExamID).Limit(1).Find(&exam)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "查询考试失败",
			})
			return
		}
		if result.RowsAffected == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "exam_id 无效",
			})
			return
		}
		if exam.NodeID == nil || *exam.NodeID != nodeIDUint {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "exam_id 不属于当前节点",
			})
			return
		}

		nodeReleaseWarning := ""
		if err := models.DB.Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&models.Exam{}).
				Where("id = ? AND end_time IS NULL", input.ExamID).
				Update("end_time", time.Now())
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return nil
			}

			nodeResult := tx.Model(&models.Node{}).
				Where("id = ? AND current_exam_id = ?", nodeIDUint, input.ExamID).
				Updates(map[string]any{
					"current_exam_id":          nil,
					"current_user_id":          nil,
					"current_user_occupied_at": nil,
					"status":                   models.NodeStatusIdle,
				})
			if nodeResult.Error != nil {
				return nodeResult.Error
			}
			if nodeResult.RowsAffected == 0 {
				nodeReleaseWarning = "节点当前考试状态已变化，考试已结束但节点未释放"
			}
			return nil
		}); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "更新考试状态失败: " + err.Error(),
			})
			return
		}

		if nodeReleaseWarning != "" {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"warning": nodeReleaseWarning,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})

	case "sync":
		// 周期同步人数
		if input.ExamID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "缺少必要参数: exam_id",
			})
			return
		}

		// 更新考场人数
		updateData := map[string]any{
			"examinee_count": input.ExamineeCount,
		}

		updateResult := models.DB.Model(&models.Exam{}).Where("id = ?", input.ExamID).Updates(updateData)
		if updateResult.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "同步人数失败",
			})
			return
		}
		if updateResult.RowsAffected == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "exam_id 无效",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "不支持的操作类型",
		})
	}
}

// ReportAlert 上报异常
func ReportAlert(c *gin.Context) {
	nodeID, _ := c.Get("node_id")

	var input struct {
		ExamID     uint    `json:"exam_id"`
		Type       string  `json:"type"`
		SeatNumber string  `json:"seat_number"`
		Message    string  `json:"message"`
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		RoomID     uint    `json:"room_id"`
	}

	// 尝试解析 JSON (模拟器通常发送 JSON)
	isJSON := false
	if strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		if err := c.ShouldBindJSON(&input); err == nil {
			isJSON = true
		}
	}

	if !isJSON {
		// 解析表单数据 (兼容老版本或带图片上传)
		input.ExamID = parseUint(c.PostForm("exam_id"))
		input.Type = c.PostForm("type")
		input.SeatNumber = c.PostForm("seat_number")
		input.Message = c.PostForm("message")
		input.X = parseFloat(c.PostForm("x"))
		input.Y = parseFloat(c.PostForm("y"))
		input.RoomID = parseUint(c.PostForm("room_id"))
	}

	if input.ExamID == 0 || input.Type == "" || input.SeatNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少必要参数: exam_id, type 或 seat_number",
		})
		return
	}

	// 处理上传的图片 (表单模式支持，JSON模式暂不支持直接传图)
	var dbPath string
	file, err := c.FormFile("image")
	if err == nil {
		// 保存图片
		uploadsDir := "./uploads/alerts"
		os.MkdirAll(uploadsDir, os.ModePerm)

		randomBuf := make([]byte, 8)
		rand.Read(randomBuf)
		ext := filepath.Ext(file.Filename)
		filename := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), hex.EncodeToString(randomBuf), ext)
		filepathStr := fmt.Sprintf("%s/%s", uploadsDir, filename)

		if err := c.SaveUploadedFile(file, filepathStr); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "保存图片失败",
			})
			return
		}
		dbPath = filepathStr
		if len(dbPath) > 1 && dbPath[0] == '.' {
			dbPath = dbPath[1:] // 去掉开头的 .
		}
	}

	// 转换nodeID类型
	nodeIDUint, ok := nodeID.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "内部错误：节点ID类型异常",
		})
		return
	}

	// 校验 exam 必须存在且必须属于当前节点
	var exam models.Exam
	result := models.DB.Where("id = ?", input.ExamID).Limit(1).Find(&exam)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询考试失败",
		})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "exam_id 无效",
		})
		return
	}
	if exam.NodeID == nil || *exam.NodeID != nodeIDUint {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "exam_id 不属于当前节点",
		})
		return
	}
	if exam.EndTime != nil {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "考试已结束，拒绝上报告警",
		})
		return
	}
	if input.RoomID != 0 && input.RoomID != exam.RoomID {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "room_id 与考试不匹配",
		})
		return
	}

	alert := models.Alert{
		ExamID:      input.ExamID,
		Type:        models.AlertType(input.Type),
		SeatNumber:  input.SeatNumber,
		X:           input.X,
		Y:           input.Y,
		Message:     input.Message,
		PicturePath: dbPath,
	}

	if alert.Message == "" {
		alert.Message = fmt.Sprintf("座位 %s 发生异常: %s", input.SeatNumber, input.Type)
	}

	if err := models.DB.Create(&alert).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建异常记录失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"alert_id": alert.ID,
	})
}

// 辅助函数：字符串转 uint
func parseUint(s string) uint {
	var result uint
	fmt.Sscanf(s, "%d", &result)
	return result
}

// 辅助函数：字符串转 float64
func parseFloat(s string) float64 {
	var result float64
	fmt.Sscanf(s, "%f", &result)
	return result
}
