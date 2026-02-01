package models

import "time"

const (
	NodeStatusIdle    = "idle"
	NodeStatusOffline = "offline"
	NodeStatusBusy    = "busy"
	NodeStatusError   = "error"
)

// Node 监考节点表。
//
// 设计说明：
// - Token 用于节点 API 鉴权（相当于机器凭证），必须唯一且保密。
// - CurrentUserID 是“事实来源”：谁占用了节点由该字段权威决定。
// - CurrentExamID 是“缓存字段”：用于快查当前考试；权威状态仍以 exams.end_time 是否为 NULL 判断。
// - LastHeartbeatAt 用于离线检测（cleanup 任务会据此把节点置为 offline）。
type Node struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"not null;unique;index" json:"name"`                                              // 节点名称
	Token   string `gorm:"not null;unique;index" json:"token"`                                             // 用于 API 鉴权
	Model   string `gorm:"not null" json:"model"`                                                          // 节点型号
	Address string `gorm:"not null" json:"address"`                                                        // 节点地址
	Status  string `gorm:"not null;index;check:status IN ('idle','offline','busy','error')" json:"status"` // idle, offline, busy, error
	Version string `gorm:"not null" json:"version"`                                                        // 软件版本

	// --- 配置状态 ---
	ConfigVersion int `json:"config_version"` // 当前配置版本号

	// --- 运行时状态与关联 ---

	// 当前正在使用的监考员 (NULL 代表没人用)
	CurrentUserID *uint `gorm:"index" json:"current_user_id"`
	CurrentUser   *User `gorm:"foreignKey:CurrentUserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"current_user,omitempty"`

	// 用户最后一次占用节点的时间（用于检测节点是否被长期占用但未释放）
	CurrentUserOccupiedAt *time.Time `json:"current_user_occupied_at"`

	// 当前正在进行的考试 (NULL 代表当前没考试)
	CurrentExamID *uint `gorm:"index" json:"current_exam_id"`
	CurrentExam   *Exam `gorm:"foreignKey:CurrentExamID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL" json:"current_exam,omitempty"`

	LastHeartbeatAt time.Time `gorm:"autoUpdateTime;index" json:"last_heartbeat_at"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
