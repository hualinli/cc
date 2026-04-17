package handlers

import (
	"bytes"
	"cc/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func CreateRoom(c *gin.Context) {
	type Input struct {
		Name     string `json:"name" binding:"required"`
		Building string `json:"building" binding:"required"`
		RTSPUrl  string `json:"rtsp_url" binding:"required"`
	}

	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "输入错误",
		})
		return
	}

	name := strings.TrimSpace(input.Name)
	building := strings.TrimSpace(input.Building)
	rtspURL := strings.TrimSpace(input.RTSPUrl)
	if name == "" || building == "" || rtspURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "教室名称、楼栋和RTSP地址不能为空",
		})
		return
	}

	room := models.Room{
		Name:     name,
		Building: building,
		RTSPUrl:  rtspURL,
	}

	if err := models.DB.Create(&room).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "创建教室失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    toRoomPayload(room),
	})
}

func DeleteRoom(c *gin.Context) {
	result := models.DB.Unscoped().Where("id = ?", c.Param("id")).Delete(&models.Room{})
	if result.Error != nil {
		if isForeignKeyConstraintError(result.Error) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"error":   "无法删除教室：存在关联考试记录",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "删除教室失败: " + result.Error.Error(),
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "教室不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func UpdateRoom(c *gin.Context) {
	type Input struct {
		Name     *string `json:"name"`
		Building *string `json:"building"`
		RTSPUrl  *string `json:"rtsp_url"`
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
				"error":   "教室名称不能为空",
			})
			return
		}
		updates["name"] = name
	}
	if input.Building != nil {
		building := strings.TrimSpace(*input.Building)
		if building == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "楼栋不能为空",
			})
			return
		}
		updates["building"] = building
	}
	if input.RTSPUrl != nil {
		rtspURL := strings.TrimSpace(*input.RTSPUrl)
		if rtspURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "RTSP地址不能为空",
			})
			return
		}
		updates["rtsp_url"] = rtspURL
	}

	if len(updates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "没有提供有效的更新字段",
		})
		return
	}

	result := models.DB.Model(&models.Room{}).Where("id = ?", c.Param("id")).Updates(updates)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "更新教室失败",
		})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "教室不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

func GetRoom(c *gin.Context) {
	var room models.Room

	if err := models.DB.Where("id = ?", c.Param("id")).First(&room).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"error":   "教室不存在",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取教室失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    toRoomPayload(room),
	})
}

func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key constraint failed")
}

func ListRooms(c *gin.Context) {
	var rooms []models.Room

	if err := models.DB.Find(&rooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   "获取教室列表失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": func() []roomPayload {
			result := make([]roomPayload, 0, len(rooms))
			for _, r := range rooms {
				result = append(result, toRoomPayload(r))
			}
			return result
		}(),
	})
}

// SyncRooms 同步教室信息到所有节点
func SyncRooms(c *gin.Context) {
	var rooms []models.Room
	if err := models.DB.Find(&rooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取教室列表失败"})
		return
	}
	// 全量sync，不区分是否online，所以可能会报错，但不影响后续流程
	var nodes []models.Node
	if err := models.DB.Find(&nodes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "获取节点列表失败"})
		return
	}

	// 准备同步数据
	type ClassroomItem struct {
		ID       uint   `json:"id"`
		Building string `json:"building"`
		Name     string `json:"name"`
		URL      string `json:"url"`
	}
	type SyncPayload struct {
		Version    string          `json:"version"`
		Classrooms []ClassroomItem `json:"classrooms"`
	}

	payload := SyncPayload{
		Version:    "1.0",
		Classrooms: make([]ClassroomItem, 0, len(rooms)),
	}
	for _, r := range rooms {
		payload.Classrooms = append(payload.Classrooms, ClassroomItem{
			ID:       r.ID,
			Building: r.Building,
			Name:     r.Name,
			URL:      r.RTSPUrl,
		})
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "数据序列化失败"})
		return
	}

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	var failures []string
	for _, node := range nodes {
		nodeURL, err := buildNodeClassroomsURL(node.Address, node.Token)
		if err != nil {
			failures = append(failures, fmt.Sprintf("节点 %s (%s): %s", node.Name, node.Address, err.Error()))
			continue
		}

		resp, err := client.Post(nodeURL, "application/json", bytes.NewReader(jsonData))

		success := false
		if err == nil {
			bodyBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				success = true
			} else {
				errorMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
				if len(bodyBytes) > 0 {
					errorMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
				}
				failures = append(failures, fmt.Sprintf("节点 %s (%s): %s", node.Name, node.Address, errorMsg))
			}
		}

		if !success {
			if err != nil {
				failures = append(failures, fmt.Sprintf("节点 %s (%s): %s", node.Name, node.Address, err.Error()))
			}
		}
	}

	if len(failures) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "部分节点同步失败",
			"errors":  failures,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "所有节点同步成功",
	})
}

func buildNodeClassroomsURL(address string, token string) (string, error) {
	trimmedAddress := strings.TrimSpace(address)
	trimmedToken := strings.TrimSpace(token)
	if trimmedAddress == "" {
		return "", errors.New("节点地址为空")
	}
	if trimmedToken == "" {
		return "", errors.New("节点令牌为空")
	}
	if strings.HasPrefix(trimmedAddress, "http://") || strings.HasPrefix(trimmedAddress, "https://") {
		return fmt.Sprintf("%s/classrooms?token=%s", strings.TrimRight(trimmedAddress, "/"), trimmedToken), nil
	}
	return fmt.Sprintf("http://%s/classrooms?token=%s", trimmedAddress, trimmedToken), nil
}
