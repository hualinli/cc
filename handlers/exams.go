package handlers

import (
	"cc/models"
	"cc/tasks"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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
	if input.ExamineeCount < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "examinee_count 不能小于 0"})
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
	if input.EndTime != nil && input.EndTime.Before(input.StartTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "end_time 必须不早于 start_time"})
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
		if input.NodeID != nil {
			var activeCount int64
			if err := tx.Model(&models.Exam{}).
				Where("node_id = ? AND end_time IS NULL", *input.NodeID).
				Count(&activeCount).Error; err != nil {
				return err
			}
			if activeCount > 0 {
				return gorm.ErrDuplicatedKey
			}
		}

		if err := tx.Create(&exam).Error; err != nil {
			if input.NodeID != nil && isConstraintConflict(err) {
				return gorm.ErrDuplicatedKey
			}
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
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "节点已被其他考试占用"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "创建考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": toExamPayload(exam)})
}

func DeleteExam(c *gin.Context) {
	idUint, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的考试ID"})
		return
	}

	var exam models.Exam
	if err := models.DB.First(&exam, uint(idUint)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "考试不存在"})
		return
	}
	if exam.EndTime == nil && exam.ScheduleStatus == models.ExamScheduleRunning {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "进行中的考试不允许删除"})
		return
	}

	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("exam_id = ?", exam.ID).Delete(&models.Alert{}).Error; err != nil {
			return err
		}

		if err := tx.Model(&models.Node{}).
			Where("current_exam_id = ?", exam.ID).
			Updates(map[string]any{
				"status":                   models.NodeStatusIdle,
				"current_exam_id":          nil,
				"current_user_id":          nil,
				"current_user_occupied_at": nil,
			}).Error; err != nil {
			return err
		}

		if err := tx.Unscoped().Delete(&models.Exam{}, exam.ID).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "删除考试失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "删除成功"})
}

func UpdateExam(c *gin.Context) {
	idUint, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的考试ID"})
		return
	}
	var exam models.Exam

	if err := models.DB.First(&exam, uint(idUint)).Error; err != nil {
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
		Remark          *string    `json:"remark"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "请求参数错误"})
		return
	}

	if exam.EndTime != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": "考试已结束，禁止更新"})
		return
	}

	isRunningOrAssigned := exam.ScheduleStatus == models.ExamScheduleRunning || exam.ScheduleStatus == models.ExamScheduleAssigned
	if isRunningOrAssigned {
		if input.RoomID != nil || input.UserID != nil || input.StartTime != nil || input.DurationSeconds != nil || input.DurationMinutes != nil {
			c.JSON(http.StatusConflict, gin.H{"success": false, "error": "考试进行中或已分配节点时，禁止修改 room_id、user_id、start_time、duration"})
			return
		}
	}

	updates := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "name 不能为空"})
			return
		}
		updates["name"] = name
	}
	if input.Subject != nil {
		subject := strings.TrimSpace(*input.Subject)
		if subject == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "subject 不能为空"})
			return
		}
		updates["subject"] = subject
	}
	if input.RoomID != nil {
		if err := models.DB.First(&models.Room{}, *input.RoomID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "room_id 无效"})
			return
		}
		updates["room_id"] = *input.RoomID
	}
	if input.UserID != nil {
		if err := models.DB.First(&models.User{}, *input.UserID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "user_id 无效"})
			return
		}
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
	if input.Remark != nil {
		updates["remark"] = strings.TrimSpace(*input.Remark)
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
	models.DB.Preload("Room").Preload("Node").Preload("User").First(&exam, uint(idUint))
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toExamPayload(exam)})
}

func GetExams(c *gin.Context) {
	idUint, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "无效的考试ID"})
		return
	}
	var exam models.Exam

	if err := models.DB.Preload("Room").Preload("Node").Preload("User").First(&exam, uint(idUint)).Error; err != nil {
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
		Remark          string       `json:"remark,omitempty"`
		CreatedAt       time.Time    `json:"created_at"`
		UpdatedAt       time.Time    `json:"updated_at"`
		Room            *roomPayload `json:"room,omitempty"`
		Node            *nodePayload `json:"node,omitempty"`
		User            *userPayload `json:"user,omitempty"`
		AnomaliesCount  int64        `json:"anomalies_count"`
	}

	var roomData *roomPayload
	if exam.Room != nil {
		r := toRoomPayload(*exam.Room)
		roomData = &r
	}
	var nodeData *nodePayload
	if exam.Node != nil {
		n := toNodePayload(*exam.Node)
		nodeData = &n
	}
	var userData *userPayload
	if exam.User != nil {
		u := toUserPayload(*exam.User)
		userData = &u
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
		Remark:          exam.Remark,
		CreatedAt:       exam.CreatedAt,
		UpdatedAt:       exam.UpdatedAt,
		Room:            roomData,
		Node:            nodeData,
		User:            userData,
		AnomaliesCount:  anomaliesCount,
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func ListExams(c *gin.Context) {
	var exams []models.Exam
	query := models.DB.Preload("Room").Preload("Node").Preload("User").
		Where("exams.schedule_status NOT IN ?", []string{models.ExamScheduleAssignFail, models.ExamScheduleNotifyFail})

	// 过滤：按楼宇 (需要关联查询)
	building := c.Query("building")
	if building != "" {
		query = query.Joins("JOIN rooms ON rooms.id = exams.room_id").Where("rooms.building = ?", building)
	}

	// 过滤：按教室
	roomID := c.Query("room_id")
	if roomID != "" {
		roomIDUint, err := strconv.ParseUint(roomID, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "room_id 无效"})
			return
		}
		query = query.Where("exams.room_id = ?", uint(roomIDUint))
	}

	// 过滤：按科目 (模糊匹配)
	subject := c.Query("subject")
	if subject != "" {
		query = query.Where("exams.subject LIKE ?", "%"+subject+"%")
	}

	// 过滤：按日期 (格式: YYYY-MM-DD)
	date := c.Query("date")
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "date 格式不合法，期望 YYYY-MM-DD"})
			return
		}
		query = query.Where("date(exams.start_time) = ?", date)
	}

	if err := query.Order("exams.id desc").Find(&exams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取考试列表失败"})
		return
	}

	result := make([]examPayload, 0, len(exams))
	for _, exam := range exams {
		result = append(result, toExamPayload(exam))
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

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
	alertCounts := make(map[uint]int64, len(ongoingExams))
	if len(examIDs) > 0 {
		rows, err := models.DB.Model(&models.Alert{}).
			Select("exam_id, COUNT(*) AS count").
			Where("exam_id IN ?", examIDs).
			Group("exam_id").Rows()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取异常统计失败"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var examID uint
			var count int64
			if err := rows.Scan(&examID, &count); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取异常统计失败"})
				return
			}
			alertCounts[examID] = count
			totalAnomalies += count
		}
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
		examsWithAnomalies = append(examsWithAnomalies, ExamWithAnomalies{
			Exam:           exam,
			AnomaliesCount: alertCounts[exam.ID],
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

	c.JSON(http.StatusOK, gin.H{"success": true, "data": toExamPayload(exam)})
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

func isConstraintConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed")
}
