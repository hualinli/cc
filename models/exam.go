package models

import "time"

const (
	ExamSchedulePending     = "pending"
	ExamScheduleAssigned    = "assigned"
	ExamScheduleNotified    = "notified"
	ExamScheduleRunning     = "running"
	ExamScheduleAssignFail  = "assign_failed"
	ExamScheduleNotifyFail  = "notify_failed"
	ExamScheduleInterrupted = "interrupted"  // 节点掉线导致考试中断
	ExamScheduleAutoClosed  = "auto_closed"  // 考试超时自动关闭
)

// Exam 考试信息表
//
// 设计原则：
// - 去除数据库外键约束，由业务逻辑保证数据一致性
// - RoomID, NodeID, UserID 均为业务外键
// - EndTime 是否为 NULL 作为考试是否结束的权威判断
// - ScheduleStatus 记录调度过程状态，用于故障排查和重试
//
// 状态转换：
// - pending: 等待调度器分配节点
// - assigned: 已分配节点，准备通知
// - notified: 已通知节点，等待节点启动确认
// - running: 节点已确认启动，考试进行中
// - assign_failed: 节点分配失败
// - notify_failed: 节点通知失败
//
// 业务约束：
// - 同一节点同一时刻仅允许一场进行中的考试
// - UNIQUE(node_id) WHERE end_time IS NULL AND node_id IS NOT NULL
type Exam struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`

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
	Remark          string     `gorm:"default:''" json:"remark,omitempty"`
	ExamineeCount   int        `gorm:"default:0" json:"examinee_count"`

	// 临时字段，用于在 handler 层传递关联数据（不存储到数据库）
	Room *Room `gorm:"-" json:"-"`
	Node *Node `gorm:"-" json:"-"`
	User *User `gorm:"-" json:"-"`
}
