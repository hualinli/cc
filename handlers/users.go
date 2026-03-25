package handlers

import (
	"cc/models"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func ListUsers(c *gin.Context) {
	var users []models.User

	if err := models.DB.Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取用户列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    users,
	})
}

func CreateUser(c *gin.Context) {
	type Input struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role" binding:"required"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}
	hashed, _ := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)

	user := models.User{
		Username: input.Username,
		Password: string(hashed),
		Role:     models.UserRole(input.Role),
	}

	if err := models.DB.Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "用户名已存在，请更换",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    user,
	})
}

func DeleteUser(c *gin.Context) {
	var user models.User

	if err := models.DB.Where("id = ?", c.Param("id")).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "用户不存在",
		})
		return
	}
	if user.Username == "admin" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无法删除admin用户",
		})
		return
	}
	if err := models.DB.Delete(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateUser(c *gin.Context) {
	var input map[string]any
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	// 如果包含密码字段，需要手动进行 Bcrypt 加密
	if pwd, ok := input["password"].(string); ok && pwd != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "密码加密失败",
			})
			return
		}
		input["password"] = string(hashed)
	}

	result := models.DB.Model(&models.User{}).Where("id = ?", c.Param("id")).Updates(input)
	if result.Error != nil {
		err := result.Error
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "用户名已被他人占用",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新用户失败",
		})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "用户不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func ChangePassword(c *gin.Context) {
	session := sessions.Default(c)
	val := session.Get("user_id")

	if val == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "用户未登录",
		})
		return
	}

	currUserID := val.(uint)

	var input struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	var user models.User
	if err := models.DB.First(&user, currUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "用户不存在",
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.OldPassword)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "旧密码错误",
		})
		return
	}

	newPassword, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新密码失败",
		})
		return
	}

	user.Password = string(newPassword)

	if err := models.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新密码失败",
		})
		return
	}

	session.Clear()
	session.Save()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}
