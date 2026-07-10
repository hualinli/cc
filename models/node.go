package models

import "time"

const (
	NodeStatusIdle    = "idle"
	NodeStatusOffline = "offline"
	NodeStatusBusy    = "busy"
	NodeStatusError   = "error"
)

// Node 监考节点表
//
// 设计原则：
// - 去除数据库外键约束，避免死锁和级联操作的性能开销
// - CurrentExamID 为业务外键，通过业务逻辑保证一致性
// - 删除 CurrentUserID 和 CurrentUserOccupiedAt，监考员通过考试间接关联节点
// - Status 是节点运行状态的唯一来源
// - 节点分配完全由调度器自动完成，监考员对节点透明
//
// 状态转换规则：
// - idle → busy: 调度器分配考试时
// - busy → idle: 考试结束时
// - any → offline: 心跳超时时
// - any → error: 节点上报错误时
//
// 并发安全：
// - 使用乐观锁更新
// - 单条 UPDATE 原子操作，避免 SELECT + UPDATE 窗口
type Node struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`

	Name      string `gorm:"not null;index" json:"name"`
	Token     string `gorm:"not null;unique" json:"token"`
	NodeModel string `json:"nodemodel"`
	Address   string `gorm:"index" json:"address"`
	Status    string `gorm:"not null;index;default:'idle'" json:"status"`
	Version   string `json:"version"`

	// 当前考试 ID
	CurrentExamID *uint `gorm:"index" json:"current_exam_id"`

	// 心跳时间
	LastHeartbeatAt time.Time `gorm:"index;not null" json:"last_heartbeat_at"`

	// 租约过期时间 - 用于检测节点掉线
	LeaseExpiresAt time.Time `gorm:"index;not null" json:"lease_expires_at"`

	// 临时字段，用于在 handler 层传递当前考试数据（不存储到数据库）
	CurrentExam *Exam `gorm:"-" json:"-"`
}
