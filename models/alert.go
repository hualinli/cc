package models

import "time"

// Alert 异常告警表
//
// 设计原则：
// - 去除数据库外键约束，由业务逻辑保证数据一致性
// - ExamID 为业务外键，指向所属考试
// - 删除策略：删除考试时，业务代码负责同时删除关联的告警记录
// - 完全信任节点上报，不做类型校验
//
// 业务规则：
// - 告警必须关联到有效的考试
// - 考试结束后不再接受新告警
type Alert struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null;index:idx_alerts_exam_created,priority:2" json:"created_at"`

	// 所属考试
	ExamID uint `gorm:"not null;index:idx_alerts_exam_created,priority:1" json:"exam_id"`

	// 告警业务字段
	Type       string  `gorm:"type:varchar(50);not null" json:"type"`
	SeatNumber string  `gorm:"type:varchar(50)" json:"seat_number"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Message    string  `gorm:"type:text" json:"message"`

	PicturePath string `gorm:"type:varchar(500)" json:"picture_path"`

	// 瞬态字段，用于在 handler 层传递关联数据（不存储到数据库）
	Exam *Exam `gorm:"-" json:"exam,omitempty"`
}

