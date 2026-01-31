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
)

func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
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
		"data":    nodes,
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
		"data":    node,
	})
}

func CreateNode(c *gin.Context) {
	type Input struct {
		Name    string `json:"name" binding:"required"`
		Model   string `json:"model" binding:"required"`
		Address string `json:"address" binding:"required"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误：请填写必填字段",
		})
		return
	}

	node := models.Node{
		Name:    input.Name,
		Token:   generateToken(),
		Model:   input.Model,
		Address: input.Address,
		Status:  models.NodeStatusIdle,
		Version: "1.0.0",
	}

	if err := models.DB.Create(&node).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "Duplicate entry") {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "节点名称已存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建节点失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    node,
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

	// 检查是否有相关的考试记录
	var examCount int64
	if err := models.DB.Model(&models.Exam{}).Where("node_id = ?", c.Param("id")).Count(&examCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "检查关联数据失败",
		})
		return
	}

	if examCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   fmt.Sprintf("无法删除节点：该节点有 %d 场相关考试记录", examCount),
		})
		return
	}

	if err := models.DB.Delete(&node).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除节点失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateNode(c *gin.Context) {
	var input map[string]any
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	updates := map[string]any{}
	if name, ok := input["name"].(string); ok && name != "" {
		updates["name"] = name
	}
	if model, ok := input["model"].(string); ok && model != "" {
		updates["model"] = model
	}
	if address, ok := input["address"].(string); ok && address != "" {
		updates["address"] = address
	}
	if err := models.DB.Model(&models.Node{}).Where("id = ?", c.Param("id")).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新节点失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
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
	updates := map[string]any{
		"status":          models.NodeStatusIdle,
		"current_user_id": nil,
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
	models.DB.Model(&models.Node{}).Where("current_user_id IS NOT NULL").Count(&occupied)
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

func ExportNodeConfig(c *gin.Context) {
	var node models.Node
	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "节点不存在",
		})
		return
	}

	// 自动获取控制中心的访问地址
	// 生产环境下建议从配置文件读一个固定的外部 IP
	ccAddr := c.Request.Host

	// SECURITY WARNING: This endpoint exposes sensitive Node Tokens.
	// In a production environment, this should be restricted to local access only,
	// or disabled entirely after the initial setup phase.
	//
	// 生产环境安全建议：完成节点初始化后，建议注释掉 main.go 中的该路由，以防 Token 泄露。

	// 拼接为 shell 脚本格式，方便 source
	content := fmt.Sprintf("#!/bin/bash\n"+
		"# Project CC Node Environment Setup\n"+
		"# Generated at: %s\n\n"+
		"export CC_SERVER_URL=\"http://%s\"\n"+
		"export NODE_ID=\"%d\"\n"+
		"export NODE_NAME=\"%s\"\n"+
		"export NODE_TOKEN=\"%s\"\n\n"+
		"echo \"[CC] Node environment variables have been set.\"\n",
		time.Now().Format("2006-01-02 15:04:05"),
		ccAddr,
		node.ID,
		node.Name,
		node.Token,
	)

	// 设置下载响应头，文件名改为 .sh
	fileName := fmt.Sprintf("setup_node_%d.sh", node.ID)
	c.Header("Content-Disposition", "attachment; filename="+fileName)
	c.Data(http.StatusOK, "text/x-sh", []byte(content))
}
