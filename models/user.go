package models

import (
	"gorm.io/gorm"
)

type UserRole string

const (
	Admin   UserRole = "admin"
	Proctor UserRole = "proctor"
)

type User struct {
	gorm.Model

	// Username 登录名，要求唯一。
	Username string `gorm:"not null;unique" json:"username"`

	// Password 密码哈希（不会通过 JSON 输出）。
	Password string `gorm:"not null" json:"-"`

	// Role 用户角色。
	Role UserRole `gorm:"default:'proctor'" json:"role"`
}
