package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	ExamSchedulePending    = "pending"
	ExamScheduleAssigned   = "assigned"
	ExamScheduleNotified   = "notified"
	ExamScheduleRunning    = "running"
	ExamScheduleAssignFail = "assign_failed"
	ExamScheduleNotifyFail = "notify_failed"
)

// Exam 考试信息表。
//
// 说明：
//   - 当前是否进行中，以 EndTime 是否为 NULL 为权威判断依据。
//   - 业务约束“同一节点同一时刻仅一场进行中考试”，建议通过 SQLite 部分唯一索引保证：
//     UNIQUE(node_id) WHERE end_time IS NULL
//     （该索引在 models.Init 中通过 Exec 创建，见 models/db.go）
type Exam struct {
	gorm.Model
	Name            string     `json:"name"`
	Subject         string     `json:"subject"`
	RoomID          uint       `gorm:"not null;index" json:"room_id"`
	NodeID          *uint      `gorm:"index" json:"node_id"`
	UserID          uint       `gorm:"not null;index" json:"user_id"`
	DurationSeconds int        `gorm:"default:0;check:duration_seconds >= 0" json:"duration_seconds"`
	StartTime       time.Time  `gorm:"not null;index" json:"start_time"`
	EndTime         *time.Time `gorm:"index" json:"end_time"`
	ScheduleStatus  string     `gorm:"not null;default:pending;index" json:"schedule_status"`
	ScheduleError   string     `json:"schedule_error,omitempty"`
	ExamineeCount   int        `gorm:"default:0" json:"examinee_count"`

	// 关联
	Room *Room `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"room,omitempty"`
	Node *Node `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"node,omitempty"`
	User *User `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"user,omitempty"`
}
