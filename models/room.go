package models

import "time"

// Room 教室表
type Room struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Building  string    `gorm:"not null;index" json:"building"`
	Name      string    `gorm:"not null;unique;index" json:"name"`
	RTSPUrl   string    `gorm:"not null" json:"rtsp_url"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
