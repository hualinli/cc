package models

import "time"

// Room 教室表。
type Room struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`

	Building string  `json:"building"`                 // 楼宇
	Name     string  `json:"name"`                     // 教室名称
	Type     *string `json:"type"`                     // 教室类型
	Remark   *string `json:"remark"`                   // 备注
	RTSPUrl  string  `gorm:"not null" json:"rtsp_url"` // RTSP地址
}
