package middleware

import (
	"cc/models"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// 鉴权中间件
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取当前 session
		session := sessions.Default(c)

		// 从 session 中获取用户信息
		userID := session.Get("user_id")

		// 如果没有用户信息，说明没有登录，重定向到登录页面
		if userID == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort() // 重要：阻止继续执行后续的 handler
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

		// 如果没有用户信息，说明没有登录
		if roleStr, ok := userRole.(string); !ok || roleStr != "admin" {
			c.AbortWithStatus(http.StatusForbidden)
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
		if err := models.DB.Where("token = ?", token).First(&node).Error; err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// 将节点信息存入 context
		c.Set("node_id", node.ID)
		c.Set("node_name", node.Name)
		c.Next()
	}
}
