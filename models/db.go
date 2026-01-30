package models

import (
	"log"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB 全局变量，整个包都能访问
var DB *gorm.DB

func Init() {
	// 打开数据库
	var err error
	DB, err = gorm.Open(sqlite.Open("cc.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database")
	}

	// 自动迁移所有表结构
	err = DB.AutoMigrate(
		&User{},
		&Room{},
		&Node{},
		&Exam{},
		&Alert{},
	)
	if err != nil {
		log.Fatal("failed to migrate database:", err)
	}

	// 初始化默认数据
	initDefaultUser()
}

func initDefaultUser() {
	// 1. 检查并创建 admin
	var admin User
	if err := DB.Where("username = ?", "admin").First(&admin).Error; err == gorm.ErrRecordNotFound {
		hashed, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		DB.Create(&User{Username: "admin", Password: string(hashed), Role: Admin})
	}
	// 2. 检查并创建 test
	var test User
	if err := DB.Where("username = ?", "test").First(&test).Error; err == gorm.ErrRecordNotFound {
		hashed, _ := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.DefaultCost)
		DB.Create(&User{Username: "test", Password: string(hashed), Role: Proctor})
	}
}
