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
	updateData := map[string]any{
		"last_heartbeat_at": time.Now(),
		"address":           c.ClientIP() + ":8002", // 心跳时自动更新节点地址为当前请求的 IP 和默认端口
	}

	// 心跳时更新节点的真实状态（idle/busy/error）
	// 但如果节点离线则不更新（由清理任务处理离线状态）
	if input.Status != "" && input.Status != models.NodeStatusOffline {
		updateData["status"] = input.Status
	}

	models.DB.Model(&models.Node{}).Where("id = ?", nodeID).Updates(updateData)

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

		// 检查节点是否被占用（指针nil检查）
		if node.CurrentUserID == nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "节点未被任何监考员占用",
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

		exam := models.Exam{
			Name:          input.Subject + "考试",
			Subject:       input.Subject,
			RoomID:        input.RoomID,
			NodeID:        nodeIDUint,
			UserID:        *node.CurrentUserID, // 当前占用节点的用户
			StartTime:     input.StartTime,
			EndTime:       nil, // 开始时结束时间仍为 NULL
			ExamineeCount: input.ExamineeCount,
		}

		if err := models.DB.Create(&exam).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "创建考试记录失败: " + err.Error(),
			})
			return
		}

		// 更新节点的当前考试ID并设置状态为 busy
		models.DB.Model(&node).Updates(map[string]any{
			"current_exam_id": exam.ID,
			"status":          models.NodeStatusBusy,
		})

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"exam_id": exam.ID,
		})

	case "stop":
		// 结束考试
		if input.ExamID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "缺少必要参数: exam_id",
			})
			return
		}

		// 更新考试结束时间
		if err := models.DB.Model(&models.Exam{}).Where("id = ?", input.ExamID).Update("end_time", time.Now()).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "更新考试状态失败",
			})
			return
		}

		// 清除节点的当前考试ID和用户ID，并恢复空闲状态
		models.DB.Model(&node).Updates(map[string]any{
			"current_exam_id": nil,
			"current_user_id": nil,
			"status":          models.NodeStatusIdle,
		})

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

		if err := models.DB.Model(&models.Exam{}).Where("id = ?", input.ExamID).Updates(updateData).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "同步人数失败",
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
	if err := models.DB.Where("id = ?", input.ExamID).First(&exam).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "exam_id 无效",
		})
		return
	}
	if exam.NodeID != nodeIDUint {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "exam_id 不属于当前节点",
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
