package models

import "time"

// AlertType 异常类型。
type AlertType string

const (
	AlertTypePhoneCheating AlertType = "phone_cheating" // 手机作弊
	AlertTypeLookAround    AlertType = "look_around"    // 东张西望
	AlertTypeWhispering    AlertType = "whispering"     // 交头接耳
	AlertTypeLeaveSheet    AlertType = "leave_sheet"    // 离开答题卡
	AlertTypeStandUp       AlertType = "stand_up"       // 站立
	AlertTypeOther         AlertType = "other"          // 其他异常
)

// Alert 异常告警表
//
// 设计原则：Alert 只强引用 Exam（Exam 再关联 Room/Node/User）。
//
// 删除策略：删除 Exam 时应级联删除其 Alert（开发阶段可由 DB CASCADE 或业务层显式删除）。
type Alert struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// 强引用：所属考试
	ExamID uint  `gorm:"index:idx_alerts_exam_created,priority:1" json:"exam_id"`
	Exam   *Exam `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"exam,omitempty"`

	// 告警业务字段
	Type       AlertType `gorm:"index:idx_alerts_exam_type_created,priority:2" json:"type"`
	SeatNumber string    `json:"seat_number"`
	X          float64   `json:"x"`
	Y          float64   `json:"y"`
	Message    string    `json:"message"`

	PicturePath string `json:"picture_path"`

	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_alerts_exam_created,priority:2;index:idx_alerts_exam_type_created,priority:3" json:"created_at"`
}
