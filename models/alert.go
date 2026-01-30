package models

import "time"

// AlertType 异常类型。
//
// 说明：当前实现用 string 枚举，便于前后端透传与扩展；是否加 CHECK 约束取决于
// 你是否希望“强约束枚举集合（更安全）”还是“允许后续新增类型无需迁移（更灵活）”。
type AlertType string

const (
	AlertTypePhoneCheating AlertType = "phone_cheating" // 手机作弊
	AlertTypeLookAround    AlertType = "look_around"    // 东张西望
	AlertTypeWhispering    AlertType = "whispering"     // 交头接耳
	AlertTypeLeaveSheet    AlertType = "leave_sheet"    // 离开答题卡
	AlertTypeStandUp       AlertType = "stand_up"       // 站立
	AlertTypeOther         AlertType = "other"          // 其他异常
)

// Alert 异常告警表（规范化）。
//
// 设计原则：Alert 只强引用 Exam（Exam 再关联 Room/Node/User）。
// 这样可以避免同一条告警同时保存 exam_id 与 room_id/node_id 造成不一致。
//
// 删除策略：删除 Exam 时应级联删除其 Alert（开发阶段可由 DB CASCADE 或业务层显式删除）。
type Alert struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// 强引用：所属考试
	ExamID uint  `gorm:"not null;index;index:idx_alerts_exam_created,priority:1" json:"exam_id"`
	Exam   *Exam `gorm:"foreignKey:ExamID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"exam,omitempty"`

	// 告警业务字段
	Type       AlertType `gorm:"type:text;not null;index;index:idx_alerts_exam_type_created,priority:2" json:"type"`
	SeatNumber string    `gorm:"not null;index" json:"seat_number"`
	X          float64   `gorm:"not null" json:"x"`
	Y          float64   `gorm:"not null" json:"y"`
	Message    string    `gorm:"not null" json:"message"`

	// PicturePath 保存相对/绝对路径均可；建议统一为以 "/uploads/..." 开头的相对路径。
	PicturePath string `gorm:"not null" json:"picture_path"`

	CreatedAt time.Time `gorm:"autoCreateTime;index;index:idx_alerts_exam_created,priority:2;index:idx_alerts_exam_type_created,priority:3" json:"created_at"`
}
