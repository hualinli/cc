package handlers

import (
	"cc/models"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func CreateNode(c *gin.Context) {
	type Input struct {
		Name      string `json:"name" binding:"required"`
		NodeModel string `json:"nodemodel"`
		Address   string `json:"address"` // 可选，不填则等待心跳自动上报
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误：请检查参数",
		})
		return
	}

	name := strings.TrimSpace(input.Name)
	model := strings.TrimSpace(input.NodeModel)
	if name == "" || model == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "节点名称和模型不能为空",
		})
		return
	}

	address := strings.TrimSpace(input.Address)
	if address == "" {
		address = "waiting_for_heartbeat"
	}

	token := generateToken()
	if token == "" {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "生成节点令牌失败",
		})
		return
	}

	node := models.Node{
		Name:      name,
		Token:     token,
		NodeModel: model,
		Address:   address,
		Status:    models.NodeStatusIdle,
		Version:   "1.0.0",
	}

	if err := models.DB.Create(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建节点失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    toNodePayload(node),
	})
}

func DeleteNode(c *gin.Context) {
	var node models.Node

	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	if node.CurrentUserID != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "无法删除节点：该节点当前正被监考员占用",
		})
		return
	}

	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Model(&models.Exam{}).
			Where("node_id = ?", node.ID).
			Updates(map[string]any{"node_id": nil}).Error; err != nil {
			return err
		}

		if err := tx.Unscoped().Where("id = ?", node.ID).Delete(&models.Node{}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if isForeignKeyConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "无法删除节点：存在关联记录",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除节点失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateNode(c *gin.Context) {
	type Input struct {
		Name      *string `json:"name"`
		NodeModel *string `json:"nodemodel"`
		Address   *string `json:"address"`
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
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "节点名称不能为空",
			})
			return
		}
		updates["name"] = name
	}
	if input.NodeModel != nil {
		model := strings.TrimSpace(*input.NodeModel)
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "节点模型不能为空",
			})
			return
		}
		updates["node_model"] = model
	}
	if input.Address != nil {
		address := strings.TrimSpace(*input.Address)
		if address == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "节点地址不能为空",
			})
			return
		}
		updates["address"] = address
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "没有提供有效的更新字段",
		})
		return
	}

	result := models.DB.Model(&models.Node{}).Where("id = ?", c.Param("id")).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新节点失败",
		})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func GetNode(c *gin.Context) {
	session := sessions.Default(c)
	userIDVal := session.Get("user_id")
	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	roleVal := session.Get("role")
	roleStr, ok := roleVal.(string)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "权限不足",
		})
		return
	}

	var node models.Node
	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 权限检查：管理员可以看到所有节点，监考员只能看到未占用或自己占用的节点
	if roleStr != "admin" {
		if node.CurrentUserID != nil && *node.CurrentUserID != userID {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "无权访问此节点",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    toNodePayload(node),
	})
}

func ListNodes(c *gin.Context) {
	session := sessions.Default(c)
	userIDVal := session.Get("user_id")
	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	roleVal := session.Get("role")
	roleStr, ok := roleVal.(string)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "权限不足",
		})
		return
	}

	var nodes []models.Node
	query := models.DB

	if roleStr == "proctor" {
		// 监考员只能看到：未被占用的节点（current_user_id IS NULL）或 自己占用的节点
		query = query.Where("current_user_id IS NULL OR current_user_id = ?", userID)
	}
	// 管理员可以看到所有节点

	if err := query.Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取节点列表失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": func() []nodePayload {
			result := make([]nodePayload, 0, len(nodes))
			for _, n := range nodes {
				result = append(result, toNodePayload(n))
			}
			return result
		}(),
	})
}

func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func GetNodeJumpURL(c *gin.Context) {
	session := sessions.Default(c)
	roleVal := session.Get("role")
	userIDVal := session.Get("user_id")

	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	role, ok := roleVal.(string)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "获取用户角色失败",
		})
		return
	}

	fmt.Printf("[DEBUG] Final userID: %d, role: %v\n", userID, role)

	var node models.Node
	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 如果是管理员，直接返回跳转 URL，不修改占用状态
	if role == "admin" {
		url := fmt.Sprintf("http://%s?token=%s", node.Address, node.Token)
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"jump_url": url,
		})
		return
	}

	// 检查并尝试锁定节点
	// 严密性逻辑：
	// 1. 如果节点未被占用且状态为空闲 (status='idle')，允许抢占
	// 2. 如果节点已经被当前用户占用，允许重入（无论 status 为何，解决刷新页面等问题）
	var updatedNode models.Node
	result := models.DB.Model(&updatedNode).
		Where("id = ? AND (current_user_id = ? OR (current_user_id IS NULL AND status = ?))", c.Param("id"), userID, models.NodeStatusIdle).
		Updates(map[string]any{
			"current_user_id":          userID,
			"current_user_occupied_at": time.Now(),
		})

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "锁定节点失败",
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "该节点已被占用或非空闲状态",
		})
		return
	}

	// 重新获取最新的节点信息（包括 Address 和 Token）
	if err := models.DB.First(&node, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 拼凑跳转 URL，携带 Token 进行简单鉴权
	url := fmt.Sprintf("http://%s?token=%s", node.Address, node.Token)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"jump_url": url,
	})
}

// ReleaseNode 释放节点占用
func ReleaseNode(c *gin.Context) {
	session := sessions.Default(c)
	roleVal := session.Get("role")
	userIDVal := session.Get("user_id")

	userID, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	role, ok := roleVal.(string)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "获取用户角色失败",
		})
		return
	}

	var node models.Node
	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 管理员可以强制释放任何节点，普通用户只能释放自己的节点
	if role != "admin" {
		if node.CurrentUserID == nil || *node.CurrentUserID != userID {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"error":   "无法释放不属于您的节点",
			})
			return
		}
	}

	// 释放节点
	if node.CurrentExamID != nil {
		var activeExam models.Exam
		if err := models.DB.Where("id = ? AND end_time IS NULL", *node.CurrentExamID).First(&activeExam).Error; err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "无法释放：该节点有进行中的考试",
			})
			return
		}
	}

	updates := map[string]any{
		"status":                   models.NodeStatusIdle,
		"current_user_id":          nil,
		"current_user_occupied_at": nil,
		"current_exam_id":          nil,
	}

	if err := models.DB.Model(&node).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "释放节点失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "节点已释放",
	})
}

func GetNodeStats(c *gin.Context) {
	var total, online, idleAvailable, busy, occupied, offline, errNodes int64

	models.DB.Model(&models.Node{}).Count(&total)
	models.DB.Model(&models.Node{}).Where("status != ?", models.NodeStatusOffline).Count(&online)
	models.DB.Model(&models.Node{}).Where("status = ? AND current_user_id IS NULL", models.NodeStatusIdle).Count(&idleAvailable)
	models.DB.Model(&models.Node{}).Where("status = ?", models.NodeStatusBusy).Count(&busy)
	models.DB.Model(&models.Node{}).
		Where("current_user_id IS NOT NULL OR current_exam_id IS NOT NULL OR status = ?", models.NodeStatusBusy).
		Count(&occupied)
	models.DB.Model(&models.Node{}).Where("status = ?", models.NodeStatusOffline).Count(&offline)
	models.DB.Model(&models.Node{}).Where("status = ?", models.NodeStatusError).Count(&errNodes)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total":          total,
			"online":         online,
			"idle_available": idleAvailable,
			"busy":           busy,
			"occupied":       occupied,
			"offline":        offline,
			"error":          errNodes,
		},
	})
}
