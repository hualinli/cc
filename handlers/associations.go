package handlers

import "cc/models"

// loadExamAssociations 加载考试的关联数据
// 参数控制加载哪些关联：loadRoom, loadNode, loadUser
func loadExamAssociations(exam *models.Exam, loadRoom, loadNode, loadUser bool) {
	if loadRoom && exam.RoomID != 0 {
		var room models.Room
		if err := models.DB.First(&room, exam.RoomID).Error; err == nil {
			exam.Room = &room
		}
	}

	if loadNode && exam.NodeID != nil {
		var node models.Node
		if err := models.DB.First(&node, *exam.NodeID).Error; err == nil {
			exam.Node = &node
		}
	}

	if loadUser && exam.UserID != 0 {
		var user models.User
		if err := models.DB.First(&user, exam.UserID).Error; err == nil {
			exam.User = &user
		}
	}
}

// loadAlertExam 加载告警关联的考试（包括考场和节点）
func loadAlertExam(alert *models.Alert) {
	if alert.ExamID == 0 {
		return
	}
	var exam models.Exam
	if err := models.DB.First(&exam, alert.ExamID).Error; err != nil {
		return
	}
	loadExamAssociations(&exam, true, true, false) // 加载 Room 和 Node
	alert.Exam = &exam
}

// loadAlertsExams 批量加载告警列表的关联考试
func loadAlertsExams(alerts []models.Alert) {
	examIDs := make([]uint, 0, len(alerts))
	for _, a := range alerts {
		if a.ExamID != 0 {
			examIDs = append(examIDs, a.ExamID)
		}
	}
	if len(examIDs) == 0 {
		return
	}

	var exams []models.Exam
	if err := models.DB.Where("id IN ?", examIDs).Find(&exams).Error; err != nil {
		return
	}

	examMap := make(map[uint]*models.Exam, len(exams))
	for i := range exams {
		loadExamAssociations(&exams[i], true, true, false)
		examMap[exams[i].ID] = &exams[i]
	}

	for i := range alerts {
		if exam, ok := examMap[alerts[i].ExamID]; ok {
			alerts[i].Exam = exam
		}
	}
}

// loadNodeCurrentExam 加载节点的当前考试（包括考试的 Room）
func loadNodeCurrentExam(node *models.Node, loadRoom bool) {
	if node.CurrentExamID == nil {
		return
	}

	var exam models.Exam
	if err := models.DB.First(&exam, *node.CurrentExamID).Error; err != nil {
		return
	}

	// 只返回进行中的考试
	if exam.EndTime == nil && node.Status != "offline" {
		if loadRoom {
			loadExamAssociations(&exam, true, false, false)
		}
		node.CurrentExam = &exam
	}
}
