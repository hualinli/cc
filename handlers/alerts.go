package handlers

import (
	"cc/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ListAlerts 获取所有异常记录
func ListAlerts(c *gin.Context) {
	var alerts []models.Alert
	query := models.DB.
		Model(&models.Alert{}).
		Joins("JOIN exams ON exams.id = alerts.exam_id").
		Preload("Exam").
		Preload("Exam.Room").
		Preload("Exam.Node")

	// 过滤：按考试ID过滤
	examID := c.Query("exam_id")
	if examID != "" {
		query = query.Where("alerts.exam_id = ?", examID)
	}

	// 过滤：按异常类型
	alertType := c.Query("type")
	if alertType != "" {
		query = query.Where("alerts.type = ?", alertType)
	}

	// 过滤：按教室ID
	roomID := c.Query("room_id")
	if roomID != "" {
		query = query.Where("exams.room_id = ?", roomID)
	}

	// 过滤：按节点ID
	nodeID := c.Query("node_id")
	if nodeID != "" {
		query = query.Where("exams.node_id = ?", nodeID)
	}

	// 过滤：按时间范围
	startTime := c.Query("start_time")
	endTime := c.Query("end_time")
	if startTime != "" {
		query = query.Where("alerts.created_at >= ?", startTime)
	}
	if endTime != "" {
		query = query.Where("alerts.created_at <= ?", endTime)
	}

	if err := query.Order("alerts.id desc").Find(&alerts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取异常记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": alerts})
}

// CreateAlert 创建新异常记录
func CreateAlert(c *gin.Context) {
	var alert models.Alert
	if err := c.ShouldBindJSON(&alert); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	// 基础校验：exam 必须存在
	var exam models.Exam
	if err := models.DB.Where("id = ?", alert.ExamID).First(&exam).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "exam_id 无效"})
		return
	}

	alert.CreatedAt = time.Now()

	if err := models.DB.Create(&alert).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "创建异常记录失败"})
		return
	}

	models.DB.Preload("Exam").Preload("Exam.Room").Preload("Exam.Node").First(&alert, alert.ID)
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
	models.DB.Preload("Exam").Preload("Exam.Room").Preload("Exam.Node").First(&alert, id)
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

// GetAlerts 获取单个异常记录详细信息
func GetAlerts(c *gin.Context) {
	id := c.Param("id")
	var alert models.Alert

	if err := models.DB.Preload("Exam").Preload("Exam.Room").Preload("Exam.Node").First(&alert, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "异常记录不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": alert})
}
