package handlers

import (
	"cc/models"
	"errors"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func LoginPostHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	var user models.User

	// TODO: 不安全，有枚举和暴力破解的风险，仅供调试
	result := models.DB.Where("username = ?", username).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "用户不存在",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "数据库错误",
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "密码错误",
		})
		return
	}

	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	session.Set("username", user.Username)
	session.Set("role", string(user.Role))
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "session 保存失败",
		})
		return
	}

	redirect := "/"
	if user.Role == models.Admin {
		redirect = "/admin/"
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"redirect": redirect,
	})
}

func LogoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "session 保存失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"redirect": "/login",
	})
}
