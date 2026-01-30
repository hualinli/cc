package models

import "time"

// AlertType 异常类型
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
type Alert struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	NodeID      uint      `gorm:"not null;index" json:"node_id"`
	RoomID      uint      `gorm:"not null;index" json:"room_id"`
	ExamID      uint      `gorm:"not null;index" json:"exam_id"`
	Type        AlertType `gorm:"type:text;not null;index" json:"type"`
	SeatNumber  string    `gorm:"not null;index" json:"seat_number"`
	X           float64   `gorm:"not null" json:"x"`
	Y           float64   `gorm:"not null" json:"y"`
	Message     string    `gorm:"not null" json:"message"`
	PicturePath string    `gorm:"not null" json:"picture_path"`
	CreatedAt   time.Time `gorm:"autoCreateTime;index" json:"created_at"`

	// 关联关系
	Node Node `gorm:"foreignKey:NodeID" json:"node,omitempty"`
	Room Room `gorm:"foreignKey:RoomID" json:"room,omitempty"`
	Exam Exam `gorm:"foreignKey:ExamID" json:"exam,omitempty"`
}
