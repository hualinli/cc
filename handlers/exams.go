package handlers

import (
	"cc/models"
	"cc/tasks"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func ListExams(c *gin.Context) {
	var exams []models.Exam
	query := models.DB.Preload("Room").Preload("Node").Preload("User")

	// 过滤：按楼宇 (需要关联查询)
	building := c.Query("building")
	if building != "" {
		query = query.Joins("JOIN rooms ON rooms.id = exams.room_id").Where("rooms.building = ?", building)
	}

	// 过滤：按教室
	roomID := c.Query("room_id")
	if roomID != "" {
		query = query.Where("exams.room_id = ?", roomID)
	}

	// 过滤：按科目 (模糊匹配)
	subject := c.Query("subject")
	if subject != "" {
		query = query.Where("exams.subject LIKE ?", "%"+subject+"%")
	}

	// 过滤：按日期 (格式: YYYY-MM-DD)
	date := c.Query("date")
	if date != "" {
		query = query.Where("date(exams.start_time) = ?", date)
	}

	if err := query.Order("exams.id desc").Find(&exams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取考试列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": exams})
}

// GetExams 获取单个考试详细信息
func GetExams(c *gin.Context) {
	id := c.Param("id")
	var exam models.Exam

	if err := models.DB.Preload("Room").Preload("Node").Preload("User").First(&exam, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "考试不存在"})
		return
	}

	var anomaliesCount int64
	models.DB.Model(&models.Alert{}).Where("exam_id = ?", exam.ID).Count(&anomaliesCount)

	type ExamResponse struct {
		ID              uint         `json:"id"`
		Name            string       `json:"name"`
		Subject         string       `json:"subject"`
		RoomID          uint         `json:"room_id"`
		NodeID          *uint        `json:"node_id"`
		UserID          uint         `json:"user_id"`
		DurationSeconds int          `json:"duration_seconds"`
		StartTime       time.Time    `json:"start_time"`
		EndTime         *time.Time   `json:"end_time"`
		ExamineeCount   int          `json:"examinee_count"`
		ScheduleStatus  string       `json:"schedule_status"`
		ScheduleError   string       `json:"schedule_error,omitempty"`
		CreatedAt       time.Time    `json:"created_at"`
		UpdatedAt       time.Time    `json:"updated_at"`
		Room            *models.Room `json:"room,omitempty"`
		Node            *models.Node `json:"node,omitempty"`
		User            *models.User `json:"user,omitempty"`
		AnomaliesCount  int64        `json:"anomalies_count"`
	}

	response := ExamResponse{
		ID:              exam.ID,
		Name:            exam.Name,
		Subject:         exam.Subject,
		RoomID:          exam.RoomID,
		NodeID:          exam.NodeID,
		UserID:          exam.UserID,
		DurationSeconds: exam.DurationSeconds,
		StartTime:       exam.StartTime,
		EndTime:         exam.EndTime,
		ExamineeCount:   exam.ExamineeCount,
		ScheduleStatus:  exam.ScheduleStatus,
		ScheduleError:   exam.ScheduleError,
		CreatedAt:       exam.CreatedAt,
		UpdatedAt:       exam.UpdatedAt,
		Room:            exam.Room,
		Node:            exam.Node,
		User:            exam.User,
		AnomaliesCount:  anomaliesCount,
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

// CreateExam 创建新考试
func CreateExam(c *gin.Context) {
	var input struct {
		Name            string     `json:"name"`
		Subject         string     `json:"subject"`
		RoomID          uint       `json:"room_id"`
		NodeID          *uint      `json:"node_id"`
		UserID          uint       `json:"user_id"`
		StartTime       time.Time  `json:"start_time"`
		EndTime         *time.Time `json:"end_time"`
		DurationSeconds int        `json:"duration_seconds"`
		DurationMinutes int        `json:"duration_minutes"`
		ExamineeCount   int        `json:"examinee_count"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	if strings.TrimSpace(input.Subject) == "" || input.RoomID == 0 || input.UserID == 0 || input.StartTime.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "缺少必要参数: subject, room_id, user_id, start_time"})
		return
	}
	durationSeconds := input.DurationSeconds
	if durationSeconds <= 0 && input.DurationMinutes > 0 {
		durationSeconds = input.DurationMinutes * 60
	}
	if durationSeconds <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "duration_seconds 必须大于 0"})
		return
	}

	if err := models.DB.First(&models.Room{}, input.RoomID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "room_id 无效"})
		return
	}
	if err := models.DB.First(&models.User{}, input.UserID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "user_id 无效"})
		return
	}
	if input.NodeID != nil {
		if err := models.DB.First(&models.Node{}, *input.NodeID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "node_id 无效"})
			return
		}
		var activeCount int64
		if err := models.DB.Model(&models.Exam{}).
			Where("node_id = ? AND end_time IS NULL", *input.NodeID).
			Count(&activeCount).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "检查节点占用失败"})
			return
		}
		if activeCount > 0 {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "该节点已有进行中考试"})
			return
		}
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = strings.TrimSpace(input.Subject) + "考试"
	}

	exam := models.Exam{
		Name:            name,
		Subject:         strings.TrimSpace(input.Subject),
		RoomID:          input.RoomID,
		NodeID:          input.NodeID,
		UserID:          input.UserID,
		StartTime:       input.StartTime,
		EndTime:         input.EndTime,
		DurationSeconds: durationSeconds,
		ExamineeCount:   input.ExamineeCount,
		ScheduleStatus:  models.ExamSchedulePending,
	}
	if input.NodeID != nil {
		exam.ScheduleStatus = models.ExamScheduleAssigned
	}

	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&exam).Error; err != nil {
			return err
		}

		if input.NodeID != nil {
			result := tx.Model(&models.Node{}).
				Where("id = ? AND current_exam_id IS NULL", *input.NodeID).
				Updates(map[string]any{
					"status":                   models.NodeStatusBusy,
					"current_exam_id":          exam.ID,
					"current_user_id":          input.UserID,
					"current_user_occupied_at": time.Now(),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrDuplicatedKey
			}
		}

		return nil
	}); err != nil {
		if err == gorm.ErrDuplicatedKey {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "节点已被其他考试占用"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "创建考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": exam})
}

// RetryAssignAndNotifyExam 管理员手动重试考试分配与通知
func RetryAssignAndNotifyExam(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的考试ID"})
		return
	}

	if err := tasks.RetryScheduleExam(uint(id)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "重试失败: " + err.Error()})
		return
	}

	var exam models.Exam
	if err := models.DB.Preload("Room").Preload("Node").Preload("User").First(&exam, uint(id)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "考试不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": exam})
}

// UpdateExam 更新考试信息
func UpdateExam(c *gin.Context) {
	id := c.Param("id")
	var exam models.Exam

	if err := models.DB.First(&exam, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "考试不存在"})
		return
	}

	var input struct {
		Name            *string    `json:"name"`
		Subject         *string    `json:"subject"`
		RoomID          *uint      `json:"room_id"`
		UserID          *uint      `json:"user_id"`
		StartTime       *time.Time `json:"start_time"`
		DurationSeconds *int       `json:"duration_seconds"`
		DurationMinutes *int       `json:"duration_minutes"`
		ExamineeCount   *int       `json:"examinee_count"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	updates := map[string]any{}
	if input.Name != nil {
		updates["name"] = strings.TrimSpace(*input.Name)
	}
	if input.Subject != nil {
		updates["subject"] = strings.TrimSpace(*input.Subject)
	}
	if input.RoomID != nil {
		updates["room_id"] = *input.RoomID
	}
	if input.UserID != nil {
		updates["user_id"] = *input.UserID
	}
	if input.StartTime != nil {
		updates["start_time"] = *input.StartTime
	}
	if input.DurationSeconds != nil {
		if *input.DurationSeconds <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "duration_seconds 必须大于 0"})
			return
		}
		updates["duration_seconds"] = *input.DurationSeconds
	} else if input.DurationMinutes != nil {
		if *input.DurationMinutes <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "duration_minutes 必须大于 0"})
			return
		}
		updates["duration_seconds"] = *input.DurationMinutes * 60
	}
	if input.ExamineeCount != nil {
		if *input.ExamineeCount < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "examinee_count 不能小于 0"})
			return
		}
		updates["examinee_count"] = *input.ExamineeCount
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "没有可更新字段"})
		return
	}

	updates["updated_at"] = time.Now()
	if err := models.DB.Model(&exam).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "更新考试失败"})
		return
	}

	// 重新加载关联数据
	models.DB.Preload("Room").Preload("Node").Preload("User").First(&exam, id)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": exam})
}

// EndExam 管理员手动结束考试
func EndExam(c *gin.Context) {
	id := c.Param("id")

	var exam models.Exam
	if err := models.DB.First(&exam, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "考试不存在"})
		return
	}

	if exam.EndTime != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "考试已结束"})
		return
	}

	now := time.Now()
	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Exam{}).
			Where("id = ? AND end_time IS NULL", exam.ID).
			Updates(map[string]any{
				"end_time":   now,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		if exam.NodeID != nil {
			if err := tx.Model(&models.Node{}).
				Where("id = ? AND current_exam_id = ?", *exam.NodeID, exam.ID).
				Updates(map[string]any{
					"status":                   models.NodeStatusIdle,
					"current_exam_id":          nil,
					"current_user_id":          nil,
					"current_user_occupied_at": nil,
				}).Error; err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "结束考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// DeleteExam 删除考试
func DeleteExam(c *gin.Context) {
	id := c.Param("id")

	// 检查是否有相关的异常记录
	var alertCount int64
	if err := models.DB.Model(&models.Alert{}).Where("exam_id = ?", id).Count(&alertCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "检查异常记录失败"})
		return
	}
	if alertCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该考试有关联的异常记录，无法删除"})
		return
	}

	// 检查是否有节点当前正在使用该考试
	var nodeCount int64
	if err := models.DB.Model(&models.Node{}).Where("current_exam_id = ?", id).Count(&nodeCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "检查节点关联失败"})
		return
	}
	if nodeCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "该考试正在被节点使用，无法删除"})
		return
	}

	// 删除考试记录
	if err := models.DB.Delete(&models.Exam{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "删除考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}

// GetExamStats 获取实时考试统计数据
func GetExamStats(c *gin.Context) {
	// 以考试表为准获取进行中考试，避免依赖节点缓存字段导致遗漏。
	var ongoingExams []models.Exam
	if err := models.DB.Where("end_time IS NULL AND schedule_status = ?", models.ExamScheduleRunning).
		Preload("Room").
		Preload("Node").
		Preload("User").
		Order("start_time asc, id asc").
		Find(&ongoingExams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取考试数据失败"})
		return
	}

	// 统计总数据
	totalRooms := len(ongoingExams)
	totalStudents := 0
	for _, exam := range ongoingExams {
		totalStudents += exam.ExamineeCount
	}

	// 统计异常总数（所有正在进行的考试）
	var totalAnomalies int64
	examIDs := make([]uint, 0, len(ongoingExams))
	for _, exam := range ongoingExams {
		examIDs = append(examIDs, exam.ID)
	}
	if len(examIDs) > 0 {
		models.DB.Model(&models.Alert{}).Where("exam_id IN ?", examIDs).Count(&totalAnomalies)
	}

	// 计算异常系数
	anomalyCoeff := 0.0
	if totalStudents > 0 {
		anomalyCoeff = float64(totalAnomalies) / float64(totalStudents)
	}

	// 为每个考试统计异常数
	type ExamWithAnomalies struct {
		models.Exam
		AnomaliesCount int64 `json:"anomalies_count"`
	}

	examsWithAnomalies := make([]ExamWithAnomalies, 0, len(ongoingExams))
	for _, exam := range ongoingExams {
		var count int64
		models.DB.Model(&models.Alert{}).Where("exam_id = ?", exam.ID).Count(&count)
		examsWithAnomalies = append(examsWithAnomalies, ExamWithAnomalies{
			Exam:           exam,
			AnomaliesCount: count,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total_rooms":     totalRooms,
			"total_students":  totalStudents,
			"total_anomalies": totalAnomalies,
			"anomaly_coeff":   anomalyCoeff,
			"ongoing_exams":   examsWithAnomalies,
		},
	})
}
