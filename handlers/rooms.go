package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func ListRooms(c *gin.Context) {
	var rooms []models.Room

	if err := models.DB.Find(&rooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取教室列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    rooms,
	})
}

func GetRoom(c *gin.Context) {
	var room models.Room

	if err := models.DB.Where("id = ?", c.Param("id")).First(&room).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "教室不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    room,
	})
}

func CreateRoom(c *gin.Context) {
	type Input struct {
		Name     string `json:"name" binding:"required"`
		Building string `json:"building" binding:"required"`
		RTSPUrl  string `json:"rtsp_url" binding:"required"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	room := models.Room{
		Name:     input.Name,
		Building: input.Building,
		RTSPUrl:  input.RTSPUrl,
	}

	if err := models.DB.Create(&room).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建教室失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    room,
	})
}

func DeleteRoom(c *gin.Context) {
	var room models.Room

	if err := models.DB.Where("id = ?", c.Param("id")).First(&room).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "教室不存在",
		})
		return
	}

	// 检查是否有相关的考试记录
	var examCount int64
	if err := models.DB.Model(&models.Exam{}).Where("room_id = ?", c.Param("id")).Count(&examCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "检查关联数据失败",
		})
		return
	}

	if examCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   fmt.Sprintf("无法删除教室：该教室有 %d 场相关考试记录", examCount),
		})
		return
	}

	if err := models.DB.Delete(&room).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除教室失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateRoom(c *gin.Context) {
	// 先检查教室是否存在
	var room models.Room
	if err := models.DB.Where("id = ?", c.Param("id")).First(&room).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "教室不存在",
		})
		return
	}

	var input map[string]any
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	updates := map[string]any{}
	if name, ok := input["name"].(string); ok && name != "" {
		updates["name"] = name
	}
	if building, ok := input["building"].(string); ok && building != "" {
		updates["building"] = building
	}
	if rtspUrl, ok := input["rtsp_url"].(string); ok && rtspUrl != "" {
		updates["rtsp_url"] = rtspUrl
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "没有提供有效的更新字段",
		})
		return
	}

	if err := models.DB.Model(&models.Room{}).Where("id = ?", c.Param("id")).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新教室失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// SyncRooms 同步教室信息到所有节点
func SyncRooms(c *gin.Context) {
	var rooms []models.Room
	if err := models.DB.Find(&rooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取教室列表失败"})
		return
	}

	var nodes []models.Node
	if err := models.DB.Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取节点列表失败"})
		return
	}

	// 准备同步数据
	type ClassroomItem struct {
		ID       uint   `json:"id"`
		Building string `json:"building"`
		Name     string `json:"name"`
		URL      string `json:"url"`
	}
	type SyncPayload struct {
		Version    string          `json:"version"`
		Classrooms []ClassroomItem `json:"classrooms"`
	}

	payload := SyncPayload{
		Version:    "1.0",
		Classrooms: make([]ClassroomItem, 0, len(rooms)),
	}
	for _, r := range rooms {
		payload.Classrooms = append(payload.Classrooms, ClassroomItem{
			ID:       r.ID,
			Building: r.Building,
			Name:     r.Name,
			URL:      r.RTSPUrl,
		})
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "数据序列化失败"})
		return
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	var failures []string
	for _, node := range nodes {
		// 忽略离线节点或特定状态的节点？用户没说，那就全量尝试
		nodeURL := fmt.Sprintf("http://%s/classrooms?token=%s", node.Address, node.Token)
		resp, err := client.Post(nodeURL, "application/json", bytes.NewBuffer(jsonData))

		success := false
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				success = true
			}
		}

		if !success {
			errorMsg := "未知错误"
			if err != nil {
				errorMsg = err.Error()
			} else if resp.StatusCode != http.StatusOK {
				errorMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			failures = append(failures, fmt.Sprintf("节点 %s (%s): %s", node.Name, node.Address, errorMsg))
		}
	}

	if len(failures) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "部分节点同步失败",
			"errors":  failures,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "所有节点同步成功",
	})
}
