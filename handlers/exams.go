package handlers

import (
	"cc/models"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GetExams 获取所有考试记录
func GetExams(c *gin.Context) {
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

	// 为每个考试统计异常数量，创建响应数据
	type ExamResponse struct {
		ID             uint         `json:"id"`
		Name           string       `json:"name"`
		Subject        string       `json:"subject"`
		RoomID         uint         `json:"room_id"`
		NodeID         uint         `json:"node_id"`
		UserID         uint         `json:"user_id"`
		StartTime      time.Time    `json:"start_time"`
		EndTime        *time.Time   `json:"end_time"`
		ExamineeCount  int          `json:"examinee_count"`
		CreatedAt      time.Time    `json:"created_at"`
		UpdatedAt      time.Time    `json:"updated_at"`
		Room           *models.Room `json:"room,omitempty"`
		Node           *models.Node `json:"node,omitempty"`
		User           *models.User `json:"user,omitempty"`
		AnomaliesCount int64        `json:"anomalies_count"`
	}

	responses := make([]ExamResponse, 0, len(exams))
	for i := range exams {
		var count int64
		models.DB.Model(&models.Alert{}).Where("exam_id = ?", exams[i].ID).Count(&count)
		responses = append(responses, ExamResponse{
			ID:             exams[i].ID,
			Name:           exams[i].Name,
			Subject:        exams[i].Subject,
			RoomID:         exams[i].RoomID,
			NodeID:         exams[i].NodeID,
			UserID:         exams[i].UserID,
			StartTime:      exams[i].StartTime,
			EndTime:        exams[i].EndTime,
			ExamineeCount:  exams[i].ExamineeCount,
			CreatedAt:      exams[i].CreatedAt,
			UpdatedAt:      exams[i].UpdatedAt,
			Room:           exams[i].Room,
			Node:           exams[i].Node,
			User:           exams[i].User,
			AnomaliesCount: count,
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": responses})
}

// GetExamStats 获取实时考试统计数据
func GetExamStats(c *gin.Context) {
	// 获取正在工作的节点（status = busy）且关联了考试的
	var busyNodes []models.Node
	if err := models.DB.Where("status = ? AND current_exam_id IS NOT NULL", models.NodeStatusBusy).
		Preload("CurrentExam").
		Preload("CurrentExam.Room").
		Preload("CurrentExam.Node").
		Find(&busyNodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取节点数据失败"})
		return
	}

	// 从 busy 节点中提取正在进行的考试
	var ongoingExams []models.Exam
	for _, node := range busyNodes {
		if node.CurrentExam != nil {
			ongoingExams = append(ongoingExams, *node.CurrentExam)
		}
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

// CreateExam 创建新考试
func CreateExam(c *gin.Context) {
	var exam models.Exam
	if err := c.ShouldBindJSON(&exam); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	// 设置创建时间
	exam.CreatedAt = time.Now()
	exam.UpdatedAt = time.Now()

	if err := models.DB.Create(&exam).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "创建考试失败"})
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

	var updates map[string]interface{}
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
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

// DeleteExam 删除考试
func DeleteExam(c *gin.Context) {
	id := c.Param("id")

	// 先删除相关的异常记录
	if err := models.DB.Where("exam_id = ?", id).Delete(&models.Alert{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "删除相关异常记录失败"})
		return
	}

	// 删除考试记录
	if err := models.DB.Delete(&models.Exam{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "删除考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}
