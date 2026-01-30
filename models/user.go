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
	ID        uint      `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"not null;unique" json:"username"`
	Password  string    `gorm:"not null" json:"-"`
	Role      UserRole  `gorm:"type:text;not null;default:'proctor';check:role IN ('admin','proctor')" json:"role"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
