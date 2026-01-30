package handlers

import (
	"cc/models"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func LoginPostHandler(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")

	var user models.User

	// TODO: 不安全，有枚举和暴力破解的风险，仅供调试
	result := models.DB.Where("username = ?", username).First(&user)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "用户不存在",
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
	session.Save()

	redirect := "/"
	if user.Role == "admin" {
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
	session.Save()

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"redirect": "/login",
	})
}
