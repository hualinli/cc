package models

import "time"

// Exam 考试信息表。
//
// 说明：
// - 当前是否进行中，以 EndTime 是否为 NULL 为权威判断依据。
// - 业务约束“同一节点同一时刻仅一场进行中考试”，建议通过 SQLite 部分唯一索引保证：
//   UNIQUE(node_id) WHERE end_time IS NULL
//   （该索引在 models.Init 中通过 Exec 创建，见 models/db.go）
type Exam struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Name          string     `gorm:"not null;index" json:"name"`       // 考试名称，添加索引
	Subject       string     `gorm:"not null;index" json:"subject"`    // 科目，添加索引以支持按科目查询
	RoomID        uint       `gorm:"not null;index" json:"room_id"`    // 教室ID
	NodeID        uint       `gorm:"not null;index;index:idx_exams_node_end,priority:1" json:"node_id"` // 节点ID
	UserID        uint       `gorm:"not null;index" json:"user_id"`    // 监考员ID (新增)
	StartTime     time.Time  `gorm:"not null;index" json:"start_time"` // 开始时间
	EndTime       *time.Time `gorm:"index;index:idx_exams_node_end,priority:2" json:"end_time"` // 结束时间（可能为空）
	ExamineeCount int        `gorm:"default:0;check:examinee_count >= 0" json:"examinee_count"` // 当前考场人数 (标定后更新)
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`

	// 关联
	Room *Room `gorm:"foreignKey:RoomID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"room,omitempty"`
	Node *Node `gorm:"foreignKey:NodeID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"node,omitempty"`
	User *User `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"user,omitempty"`
}
