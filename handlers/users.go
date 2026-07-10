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
		"data":    toUserPayload(user),
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
		"data":    toUserPayload(user),
	})
}

func ListUsers(c *gin.Context) {
	// 分页参数
	page, pageSize := parsePaginationParams(c)
	offset := (page - 1) * pageSize

	var total int64
	if err := models.DB.Model(&models.User{}).Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "查询用户总数失败",
		})
		return
	}

	var users []models.User
	if err := models.DB.Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取用户列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": func() []userPayload {
			result := make([]userPayload, 0, len(users))
			for _, u := range users {
				result = append(result, toUserPayload(u))
			}
			return result
		}(),
		"pagination": gin.H{
			"page":       page,
			"page_size":  pageSize,
			"total":      total,
			"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
		},
	})
}

func DeleteUser(c *gin.Context) {
	userID := c.Param("id")

	// 检查是否是 admin 用户
	var user models.User
	if err := models.DB.First(&user, userID).Error; err != nil {
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

	if user.Username == "admin" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无法禁用管理员账户",
		})
		return
	}

	// 检查是否有正在进行的考试
	var runningExamCount int64
	models.DB.Model(&models.Exam{}).
		Where("user_id = ? AND end_time IS NULL", userID).
		Count(&runningExamCount)

	if runningExamCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "该用户有正在进行的考试，无法禁用",
		})
		return
	}

	// 禁用用户
	result := models.DB.Model(&user).Update("status", models.UserStatusDisabled)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "禁用用户失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// ForceDeleteUser 强制级联删除用户及其所有关联数据
// 危险操作：会删除用户的所有考试记录、告警等数据
func ForceDeleteUser(c *gin.Context) {
	userID := c.Param("id")

	// 检查是否是 admin 用户
	var user models.User
	if err := models.DB.First(&user, userID).Error; err != nil {
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

	if user.Username == "admin" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无法删除管理员账户",
		})
		return
	}

	// 检查是否有正在进行的考试
	var runningExamCount int64
	models.DB.Model(&models.Exam{}).
		Where("user_id = ? AND end_time IS NULL", userID).
		Count(&runningExamCount)

	if runningExamCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "该用户有正在进行的考试，无法删除",
		})
		return
	}

	// 级联删除
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 查询用户的所有考试 ID
		var examIDs []uint
		tx.Model(&models.Exam{}).Where("user_id = ?", userID).Pluck("id", &examIDs)

		if len(examIDs) > 0 {
			// 2. 删除这些考试的所有告警
			if err := tx.Where("exam_id IN ?", examIDs).Delete(&models.Alert{}).Error; err != nil {
				return err
			}

			// 3. 清空节点的 current_exam_id（如果指向这些考试）
			if err := tx.Model(&models.Node{}).
				Where("current_exam_id IN ?", examIDs).
				Update("current_exam_id", nil).Error; err != nil {
				return err
			}

			// 4. 删除所有考试
			if err := tx.Where("user_id = ?", userID).Delete(&models.Exam{}).Error; err != nil {
				return err
			}
		}

		// 5. 删除用户
		if err := tx.Delete(&user).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除用户失败: " + err.Error(),
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
		Status   *string `json:"status"`
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

	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != models.UserStatusActive && status != models.UserStatusDisabled {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "状态值非法",
			})
			return
		}
		updates["status"] = status
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
