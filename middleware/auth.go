package middleware

import (
	"cc/models"
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// 鉴权中间件
func isJSONRequest(c *gin.Context) bool {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/node-api") {
		return true
	}
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	return false
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取当前 session
		session := sessions.Default(c)

		// 从 session 中获取用户信息
		userID := session.Get("user_id")

		// 如果没有用户信息，说明没有登录，API 请求返回 401，否则跳转到登录页面。
		if userID == nil {
			if isJSONRequest(c) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			} else {
				c.Redirect(http.StatusFound, "/login")
				c.Abort()
			}
			return
		}

		// 已登录，将请求传给下一个环节
		c.Next()
	}
}

func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取当前 session
		session := sessions.Default(c)

		// 从 session 中获取用户信息
		userRole := session.Get("role")

		// 如果没有用户信息，说明没有登录或不是管理员
		if roleStr, ok := userRole.(string); !ok || roleStr != "admin" {
			if isJSONRequest(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			} else {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return
		}

		// 已登录，将请求传给下一个环节
		c.Next()
	}
}

func NodeAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Node-Token")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}
		var node models.Node
		// 使用 Limit(1).Find 而不是 First 可以避免在找不到记录时触发 GORM 的错误日志
		result := models.DB.Where("token = ?", token).Limit(1).Find(&node)
		if result.Error != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "database error"})
			return
		}

		if result.RowsAffected == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// 将节点信息存入 context
		c.Set("node_id", node.ID)
		c.Set("node_name", node.Name)
		c.Next()
	}
}
