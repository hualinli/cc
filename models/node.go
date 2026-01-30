package models

import "time"

const (
	NodeStatusIdle    = "idle"
	NodeStatusOffline = "offline"
	NodeStatusBusy    = "busy"
	NodeStatusError   = "error"
)

// Node 监考节点表
type Node struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"not null;unique;index" json:"name"`
	Token   string `gorm:"not null;unique;index" json:"token"` // 用于 API 鉴权
	Model   string `gorm:"not null" json:"model"`
	Address string `gorm:"not null" json:"address"`
	Status  string `gorm:"not null;index" json:"status"` // idle, offline, busy, error
	Version string `gorm:"not null" json:"version"`      // 软件版本

	// --- 配置状态 ---
	ConfigVersion int `json:"config_version"` // 当前配置版本号

	// --- 运行时状态与关联 ---

	// 当前正在使用的监考员 (NULL 代表没人用)
	CurrentUserID *uint `gorm:"index" json:"current_user_id"`
	CurrentUser   *User `gorm:"foreignKey:CurrentUserID" json:"current_user,omitempty"`

	// 用户最后一次占用节点的时间（用于检测节点是否被长期占用但未释放）
	CurrentUserOccupiedAt *time.Time `json:"current_user_occupied_at"`

	// 当前正在进行的考试 (NULL 代表当前没考试)
	CurrentExamID *uint `gorm:"index" json:"current_exam_id"`
	CurrentExam   *Exam `gorm:"foreignKey:CurrentExamID" json:"current_exam,omitempty"`

	LastHeartbeatAt time.Time `gorm:"autoUpdateTime;index" json:"last_heartbeat_at"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
