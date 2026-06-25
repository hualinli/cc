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
		Name:            name,
		Token:           token,
		NodeModel:       model,
		Address:         address,
		Status:          models.NodeStatusIdle,
		Version:         "1.0.0",
		LastHeartbeatAt: time.Now(),
		LeaseExpiresAt:  time.Now().Add(2 * time.Minute), // 初始租约 2 分钟
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

	// 检查是否有进行中的考试
	var runningExamCount int64
	models.DB.Model(&models.Exam{}).
		Where("node_id = ? AND end_time IS NULL", node.ID).
		Count(&runningExamCount)

	if runningExamCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"error":   "无法删除节点：该节点有正在进行的考试",
		})
		return
	}

	if err := models.DB.Transaction(func(tx *gorm.DB) error {
		// 清空已结束考试的 node_id 引用
		if err := tx.Model(&models.Exam{}).
			Where("node_id = ?", node.ID).
			Updates(map[string]any{"node_id": nil}).Error; err != nil {
			return err
		}

		// 删除节点
		if err := tx.Delete(&node).Error; err != nil {
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
	_, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	roleVal := session.Get("role")
	_, ok = roleVal.(string)
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

	// 加载当前考试信息（包括考场）
	loadNodeCurrentExam(&node, true)

	// 监考员权限检查已移除：CurrentUserID 字段已删除，节点与监考员的关联通过考试间接管理。

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    toNodePayloadWithExam(node),
	})
}

func ListNodes(c *gin.Context) {
	session := sessions.Default(c)
	userIDVal := session.Get("user_id")
	_, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	roleVal := session.Get("role")
	_, ok = roleVal.(string)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "权限不足",
		})
		return
	}

	// 分页参数
	page, pageSize := parsePaginationParams(c)

	var nodes []models.Node
	var total int64
	query := models.DB.Model(&models.Node{})

	// 监考员节点过滤已移除：CurrentUserID 字段已删除，节点列表对所有已认证用户可见。

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取节点总数失败",
		})
		return
	}

	// 分页查询，按 ID 降序
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset).Find(&nodes).Error; err != nil {
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
		"pagination": gin.H{
			"page":       page,
			"page_size":  pageSize,
			"total":      total,
			"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
		},
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

	_, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	_, ok = roleVal.(string)
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

	// 检查节点地址是否可用
	if node.Address == "" || node.Address == "waiting_for_heartbeat" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "节点地址不可用，请等待节点心跳上报",
		})
		return
	}

	jumpURL := fmt.Sprintf("http://%s/", node.Address)
	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"jump_url": jumpURL,
	})
}

// ReleaseNode 释放节点占用
func ReleaseNode(c *gin.Context) {
	session := sessions.Default(c)
	roleVal := session.Get("role")
	userIDVal := session.Get("user_id")

	_, ok := userIDVal.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "user_id 类型错误",
		})
		return
	}

	_, ok = roleVal.(string)
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

	// CurrentUserID 权限检查已移除：管理员和监考员均可释放节点，节点的使用由考试调度器管理。

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
		"status":          models.NodeStatusIdle,
		"current_exam_id": nil,
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
	models.DB.Model(&models.Node{}).Where("status = ?", models.NodeStatusIdle).Count(&idleAvailable)
	models.DB.Model(&models.Node{}).Where("status = ?", models.NodeStatusBusy).Count(&busy)
	// 占用状态通过 current_exam_id 判断
	models.DB.Model(&models.Node{}).
		Where("current_exam_id IS NOT NULL OR status = ?", models.NodeStatusBusy).
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
