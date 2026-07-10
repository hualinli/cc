package models

import (
	"time"
)

type UserRole string

const (
	Admin   UserRole = "admin"
	Proctor UserRole = "proctor"
)

const (
	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type User struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`

	Username string   `gorm:"not null;unique" json:"username"`
	Password string   `gorm:"not null" json:"-"`
	Role     UserRole `gorm:"default:'proctor'" json:"role"`
	Status   string   `gorm:"default:'active'" json:"status"`
}
