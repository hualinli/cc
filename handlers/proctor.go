package handlers

import (
	"cc/models"
	"net/http"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ListProctorExams 获取监考员的考试列表
func ListProctorExams(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "未登录"})
		return
	}

	// 分页参数
	page, pageSize := parsePaginationParams(c)

	// 筛选参数
	building := c.Query("building")   // 楼宇
	date := c.Query("date")            // 日期 (YYYY-MM-DD)
	status := c.Query("status")        // 状态: upcoming, ongoing, completed

	// 构建查询
	query := models.DB.Where("user_id = ?", userID)

	// 楼宇筛选
	if building != "" {
		query = query.Joins("JOIN rooms ON rooms.id = exams.room_id").
			Where("rooms.building = ?", building)
	}

	// 日期筛选
	if date != "" {
		parsedDate, err := time.Parse("2006-01-02", date)
		if err == nil {
			startOfDay := parsedDate
			endOfDay := parsedDate.Add(24 * time.Hour)
			query = query.Where("start_time >= ? AND start_time < ?", startOfDay, endOfDay)
		}
	}

	// 状态筛选
	now := time.Now()
	switch status {
	case "upcoming":
		// 未开始
		query = query.Where("start_time > ?", now)
	case "ongoing":
		// 进行中
		query = query.Where("start_time <= ? AND end_time IS NULL", now)
	case "completed":
		// 已结束
		query = query.Where("end_time IS NOT NULL")
	}

	// 获取总数
	var total int64
	countQuery := query
	if err := countQuery.Model(&models.Exam{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "查询失败"})
		return
	}

	// 分页查询
	var exams []models.Exam
	offset := (page - 1) * pageSize
	err := query.
		Order("start_time DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&exams).Error

	// 手动加载关联数据
	for i := range exams {
		loadExamAssociations(&exams[i], true, true, false)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    exams,
		"pagination": gin.H{
			"page":       page,
			"page_size":  pageSize,
			"total":      total,
			"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
		},
	})
}

// GetProctorExamStats 获取监考员的考试统计
func GetProctorExamStats(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "未登录"})
		return
	}

	now := time.Now()

	// 总考试数
	var totalExams int64
	models.DB.Model(&models.Exam{}).Where("user_id = ?", userID).Count(&totalExams)

	// 进行中的考试数
	var ongoingExams int64
	models.DB.Model(&models.Exam{}).
		Where("user_id = ? AND start_time <= ? AND end_time IS NULL", userID, now).
		Count(&ongoingExams)

	// 未开始的考试数
	var upcomingExams int64
	models.DB.Model(&models.Exam{}).
		Where("user_id = ? AND start_time > ?", userID, now).
		Count(&upcomingExams)

	// 已完成的考试数
	var completedExams int64
	models.DB.Model(&models.Exam{}).
		Where("user_id = ? AND end_time IS NOT NULL", userID).
		Count(&completedExams)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total":     totalExams,
			"ongoing":   ongoingExams,
			"upcoming":  upcomingExams,
			"completed": completedExams,
		},
	})
}
