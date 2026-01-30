package models

import "time"

// Room 教室表。
//
// 约束建议：
// - 生产语义通常不允许删除 Room（避免历史 Exam 断链），建议业务层不提供删除接口，DB 层可 RESTRICT。
// - Name 当前为全局唯一；如果你未来希望“不同楼栋可同名”，可改为 (building, name) 联合唯一。
type Room struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Building  string    `gorm:"not null;index" json:"building"`    // 楼栋
	Name      string    `gorm:"not null;unique;index" json:"name"` // 教室名称
	RTSPUrl   string    `gorm:"not null" json:"rtsp_url"`          // RTSP地址
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
