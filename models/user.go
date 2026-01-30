package models

import (
	"time"
)

type UserRole string

const (
	Admin   UserRole = "admin"
	Proctor UserRole = "proctor"
)

type User struct {
	// ID 自增主键。
	ID uint `gorm:"primaryKey" json:"id"`

	// Username 登录名，要求唯一。
	Username string `gorm:"not null;unique" json:"username"`

	// Password 密码哈希（不会通过 JSON 输出）。
	Password string `gorm:"not null" json:"-"`

	// Role 用户角色。
	// 说明：用 CHECK 限定值域，避免写入脏数据。
	Role UserRole `gorm:"type:text;not null;default:'proctor';check:role IN ('admin','proctor')" json:"role"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
