package handlers

import (
	"cc/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetAlerts 获取所有异常记录
func GetAlerts(c *gin.Context) {
	var alerts []models.Alert
	query := models.DB.Preload("Room").Preload("Node").Preload("Exam")

	// 过滤：按考试ID过滤
	examID := c.Query("exam_id")
	if examID != "" {
		query = query.Where("exam_id = ?", examID)
	}

	// 过滤：按异常类型
	alertType := c.Query("type")
	if alertType != "" {
		query = query.Where("type = ?", alertType)
	}

	// 过滤：按教室ID
	roomID := c.Query("room_id")
	if roomID != "" {
		query = query.Where("room_id = ?", roomID)
	}

	// 过滤：按节点ID
	nodeID := c.Query("node_id")
	if nodeID != "" {
		query = query.Where("node_id = ?", nodeID)
	}

	// 过滤：按时间范围
	startTime := c.Query("start_time")
	endTime := c.Query("end_time")
	if startTime != "" {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime != "" {
		query = query.Where("created_at <= ?", endTime)
	}

	if err := query.Order("id desc").Find(&alerts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取异常记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": alerts})
}

// GetAlertStats 获取异常统计数据
func GetAlertStats(c *gin.Context) {
	examID := c.Query("exam_id")

	var query = models.DB.Model(&models.Alert{})
	if examID != "" {
		query = query.Where("exam_id = ?", examID)
	}

	// 按类型统计
	type TypeCount struct {
		Type  string `json:"type"`
		Count int64  `json:"count"`
	}
	var typeCounts []TypeCount
	query.Select("type, count(*) as count").Group("type").Scan(&typeCounts)

	// 总数统计
	var totalCount int64
	query.Count(&totalCount)

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"total_count": totalCount,
		"by_type":     typeCounts,
	})
}

// CreateAlert 创建新异常记录
func CreateAlert(c *gin.Context) {
	var alert models.Alert
	if err := c.ShouldBindJSON(&alert); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	alert.CreatedAt = time.Now()

	if err := models.DB.Create(&alert).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "创建异常记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": alert})
}

// UpdateAlert 更新异常记录
func UpdateAlert(c *gin.Context) {
	id := c.Param("id")
	var alert models.Alert

	if err := models.DB.First(&alert, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "异常记录不存在"})
		return
	}

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	if err := models.DB.Model(&alert).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "更新异常记录失败"})
		return
	}

	// 重新加载关联数据
	models.DB.Preload("Room").Preload("Node").Preload("Exam").First(&alert, id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": alert})
}

// DeleteAlert 删除异常记录
func DeleteAlert(c *gin.Context) {
	id := c.Param("id")

	if err := models.DB.Delete(&models.Alert{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "删除异常记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}
