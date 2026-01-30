package handlers

import (
	"cc/models"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
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

// 明确的返回结构，确保 JSON 序列化时字段持平且名称一致
type NodeResp struct {
	ID              uint      `json:"id"`
	Name            string    `json:"name"`
	Model           string    `json:"model"`
	Address         string    `json:"address"`
	Status          string    `json:"status"`
	Version         string    `json:"version"`
	CurrentUserID   *uint     `json:"current_user_id"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at"`
	IsAssignedToMe  bool      `json:"is_assigned_to_me"`
}

func GetNodes(c *gin.Context) {
	session := sessions.Default(c)
	role := session.Get("role")
	userIDVal := session.Get("user_id")

	var userID uint
	switch v := userIDVal.(type) {
	case uint:
		userID = v
	case int:
		userID = uint(v)
	case float64:
		userID = uint(v)
	}

	var nodes []models.Node
	query := models.DB

	if role == "proctor" {
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

	resp := make([]NodeResp, 0, len(nodes))
	for _, n := range nodes {
		assigned := false
		if n.CurrentUserID != nil && *n.CurrentUserID == userID {
			assigned = true
		}
		resp = append(resp, NodeResp{
			ID:              n.ID,
			Name:            n.Name,
			Model:           n.Model,
			Address:         n.Address,
			Status:          n.Status,
			Version:         n.Version,
			CurrentUserID:   n.CurrentUserID,
			LastHeartbeatAt: n.LastHeartbeatAt,
			IsAssignedToMe:  assigned,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

func CreateNode(c *gin.Context) {
	type Input struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		Address string `json:"address"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
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
	role := session.Get("role")
	userIDVal := session.Get("user_id")

	var userID uint
	switch v := userIDVal.(type) {
	case uint:
		userID = v
	case int:
		userID = uint(v)
	case float64:
		userID = uint(v)
	default:
		// 调试日志
		fmt.Printf("[DEBUG] userIDVal type: %T, value: %v\n", userIDVal, userIDVal)
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

	// 检查并锁定节点（监考员和管理员都需要）
	if node.CurrentUserID != nil && *node.CurrentUserID != userID {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "该节点已被其他用户占用",
		})
		return
	}

	// 锁定节点，记录占用时间和用户
	// 注意：不强制设置 status='busy'，让节点心跳返回真实的 busy/idle 状态
	now := time.Now()
	updates := map[string]any{
		"current_user_id":          userID,
		"current_user_occupied_at": now,
	}

	fmt.Printf("[DEBUG] Updating node %d with userID: %d\n", node.ID, userID)

	if err := models.DB.Model(&node).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "分配节点失败",
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
	role := session.Get("role")
	userIDVal := session.Get("user_id")

	var userID uint
	switch v := userIDVal.(type) {
	case uint:
		userID = v
	case int:
		userID = uint(v)
	case float64:
		userID = uint(v)
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

func ExportNodeConfig(c *gin.Context) {
	var node models.Node
	if err := models.DB.Where("id = ?", c.Param("id")).First(&node).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
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
