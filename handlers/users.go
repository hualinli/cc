package handlers

import (
	"cc/models"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

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

	username := strings.TrimSpace(input.Username)
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "用户名不能为空",
		})
		return
	}

	if strings.TrimSpace(input.Password) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "密码不能为空",
		})
		return
	}

	role := strings.TrimSpace(input.Role)
	if role != string(models.Admin) && role != string(models.Proctor) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "角色非法",
		})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "密码加密失败",
		})
		return
	}
	user := models.User{
		Username: username,
		Password: string(hashed),
		Role:     models.UserRole(role),
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

func GetUser(c *gin.Context) {
	var user models.User

	if err := models.DB.Where("id = ?", c.Param("id")).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "用户不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    user,
	})
}

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

func DeleteUser(c *gin.Context) {
	result := models.DB.Unscoped().Where("id = ? AND username <> ?", c.Param("id"), "admin").Delete(&models.User{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除用户失败",
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "用户不存在或无法删除",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateUser(c *gin.Context) {
	type Input struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		Role     *string `json:"role"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	updates := map[string]any{}

	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "用户名不能为空",
			})
			return
		}
		updates["username"] = username
	}

	if input.Password != nil {
		if *input.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "密码不能为空",
			})
			return
		}

		hashed, err := bcrypt.GenerateFromPassword([]byte(*input.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "密码加密失败",
			})
			return
		}
		updates["password"] = string(hashed)
	}

	if input.Role != nil {
		role := strings.TrimSpace(*input.Role)
		if role != string(models.Admin) && role != string(models.Proctor) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "角色非法",
			})
			return
		}
		updates["role"] = models.UserRole(role)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "没有可更新字段",
		})
		return
	}

	result := models.DB.Model(&models.User{}).Where("id = ?", c.Param("id")).Updates(updates)
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

	currUserID, ok := parseSessionUserID(val)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"error":   "用户未登录",
		})
		return
	}

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

	if strings.TrimSpace(input.OldPassword) == "" || strings.TrimSpace(input.NewPassword) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	var user models.User
	if err := models.DB.First(&user, currUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "用户不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询用户失败",
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
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "会话清理失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func parseSessionUserID(v any) (uint, bool) {
	switch id := v.(type) {
	case uint:
		return id, true
	case uint64:
		return uint(id), true
	case int:
		if id < 0 {
			return 0, false
		}
		return uint(id), true
	case int64:
		if id < 0 {
			return 0, false
		}
		return uint(id), true
	case float64:
		if id < 0 {
			return 0, false
		}
		return uint(id), true
	case string:
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return 0, false
		}
		return uint(n), true
	default:
		return 0, false
	}
}
