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
	nodeID, exist := c.Get("node_id")
	if !exist {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "未提供节点ID",
		})
		return
	}
	nodeIDUint, ok := nodeID.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "内部错误：节点ID类型异常",
		})
		return
	}

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
		case models.NodeStatusIdle, models.NodeStatusBusy, models.NodeStatusError:
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
	if err := models.DB.Where("id = ?", nodeIDUint).First(&node).Error; err != nil {
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

	// 心跳仅接收节点运行态（idle/busy/error）。
	// offline 由 cleanup 根据超时统一写入，避免节点自行宣告离线。
	if input.Status != "" {
		switch input.Status {
		case models.NodeStatusIdle:
			// 节点上报 idle：仅更新 node 侧状态与占用字段。
			// 其他表的收敛（如自动关考）交由 cleanup 任务统一处理。
			clearNodeOccupation(updateData)
			setNodeStatusIfChanged(updateData, node.Status, models.NodeStatusIdle)
		case models.NodeStatusBusy:
			setNodeStatusIfChanged(updateData, node.Status, input.Status)
		case models.NodeStatusError:
			clearNodeOccupation(updateData)
			setNodeStatusIfChanged(updateData, node.Status, models.NodeStatusError)
		}
	}

	if err := models.DB.Model(&models.Node{}).Where("id = ?", nodeIDUint).UpdateColumns(updateData).Error; err != nil {
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
	nodeID, exist := c.Get("node_id")
	if !exist {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "未提供节点ID",
		})
		return
	}
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
		// 幂等确认：节点重复上报时只校验 exam 是否属于本节点并返回。
		if input.ExamID != 0 {
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
					"error":   "考试已结束",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{"success": true, "exam_id": exam.ID})
			return
		}

		// 正常开考：校验房间存在后创建考试并返回 exam_id。
		if input.RoomID == 0 || input.Subject == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "缺少必要参数: room_id 或 subject",
			})
			return
		}

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
			if currentNode.CurrentUserID == nil {
				return fmt.Errorf("节点未被任何监考员占用")
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

			now := time.Now()
			if err := tx.Model(&currentNode).Updates(map[string]any{
				"current_exam_id":          exam.ID,
				"current_user_occupied_at": now,
				"status":                   models.NodeStatusBusy,
			}).Error; err != nil {
				return err
			}

			responseExamID = exam.ID
			return nil
		})
		if txErr != nil {
			status := mapSyncTaskStartErrorStatus(txErr.Error())
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

		var exam models.Exam
		examResult := models.DB.Where("id = ?", input.ExamID).Limit(1).Find(&exam)
		if examResult.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "查询考试失败",
			})
			return
		}
		if examResult.RowsAffected == 0 {
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
				"error":   "考试已结束",
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

	if !isValidAlertType(models.AlertType(input.Type)) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "type 无效",
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

func setNodeStatusIfChanged(updateData map[string]any, currentStatus string, targetStatus string) {
	if currentStatus != targetStatus {
		updateData["status"] = targetStatus
	}
}

func clearNodeOccupation(updateData map[string]any) {
	updateData["current_exam_id"] = nil
	updateData["current_user_id"] = nil
	updateData["current_user_occupied_at"] = nil
}

func mapSyncTaskStartErrorStatus(errMsg string) int {
	switch errMsg {
	case "节点未被任何监考员占用", "exam_id 无效", "room_id 与考试不匹配":
		return http.StatusBadRequest
	case "exam_id 不属于当前节点":
		return http.StatusForbidden
	case "考试已结束":
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
