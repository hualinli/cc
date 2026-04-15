package models

import "gorm.io/gorm"

// Room 教室表。
//
// - 生产语义通常不允许删除 Room（避免历史 Exam 断链），建议业务层不提供删除接口。
type Room struct {
	gorm.Model
	Building string  `json:"building"`  // 楼宇
	Name     string  `json:"name"`      // 教室名称
	Type     *string `json:"type"`      // 教室类型
	Remark   *string `json:"remark"`    // 备注
	RTSPUrl  string  `gorm:"not null" json:"rtsp_url"` // RTSP地址
}
