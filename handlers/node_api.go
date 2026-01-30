package handlers

import (
	"cc/models"
	"fmt"
	"net/http"
	"os"
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
		})
		return
	}

	// 查询当前节点状态
	var node models.Node
	if err := models.DB.Where("id = ?", nodeID).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
		})
		return
	}

	// 更新数据库状态
	updateData := map[string]any{
		"last_heartbeat_at": time.Now(),
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
			"error":   "请求参数错误",
		})
		return
	}

	// 获取节点信息
	var node models.Node
	if err := models.DB.Where("id = ?", nodeID).First(&node).Error; err != nil {
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

		// 创建考试记录
		var nodeIDUint uint
		switch v := nodeID.(type) {
		case uint:
			nodeIDUint = v
		case int:
			nodeIDUint = uint(v)
		case float64:
			nodeIDUint = uint(v)
		}
		exam := models.Exam{
			Name:          input.Subject + "考试",
			Subject:       input.Subject,
			RoomID:        input.RoomID,
			NodeID:        nodeIDUint,
			UserID:        *node.CurrentUserID, // 当前占用节点的用户
			StartTime:     input.StartTime,
			EndTime:       nil, // 开始时结束时间仍为 NULL
			ExamineeCount: 0,
		}

		if err := models.DB.Create(&exam).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "创建考试记录失败",
			})
			return
		}

		// 更新节点的当前考试ID
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

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

		// 清除节点的当前考试ID和用户ID
		models.DB.Model(&node).Updates(map[string]any{
			"current_exam_id": nil,
			"current_user_id": nil,
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

	// 解析表单数据
	roomID := c.PostForm("room_id")
	examID := c.PostForm("exam_id")
	alertType := c.PostForm("type")
	seatNumber := c.PostForm("seat_number")
	x := c.PostForm("x")
	y := c.PostForm("y")

	if roomID == "" || examID == "" || alertType == "" || seatNumber == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少必要参数",
		})
		return
	}

	// 处理上传的图片
	file, err := c.FormFile("image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "缺少图片文件",
		})
		return
	}

	// 保存图片
	uploadsDir := "./uploads/alerts"
	os.MkdirAll(uploadsDir, os.ModePerm)
	filename := fmt.Sprintf("%d_%s", time.Now().Unix(), file.Filename)
	filepath := fmt.Sprintf("%s/%s", uploadsDir, filename)

	if err := c.SaveUploadedFile(file, filepath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "保存图片失败",
		})
		return
	}

	// 转换nodeID类型
	var nodeIDUint uint
	switch v := nodeID.(type) {
	case uint:
		nodeIDUint = v
	case int:
		nodeIDUint = uint(v)
	case float64:
		nodeIDUint = uint(v)
	}

	// 创建异常记录
	dbPath := filepath
	if len(dbPath) > 1 && dbPath[0] == '.' {
		dbPath = dbPath[1:] // 去掉开头的 .
	}

	alert := models.Alert{
		NodeID:      nodeIDUint,
		RoomID:      parseUint(roomID),
		ExamID:      parseUint(examID),
		Type:        models.AlertType(alertType),
		SeatNumber:  seatNumber,
		X:           parseFloat(x),
		Y:           parseFloat(y),
		Message:     fmt.Sprintf("座位 %s 发生异常: %s", seatNumber, alertType),
		PicturePath: dbPath,
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
