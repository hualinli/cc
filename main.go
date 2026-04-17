package main

import (
	"cc/handlers"
	"cc/middleware"
	"cc/models"
	"cc/tasks"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func main() {
	models.Init()
	tasks.StartCleanupTask()
	tasks.StartExamScheduler()
	r := gin.Default()

	store := cookie.NewStore([]byte("secret-key"))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 4, // 4 hours
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("session", store))

	// 静态文件
	r.Static("/css", "./template/css")
	r.Static("/js", "./template/js")
	r.Static("/webfonts", "./template/webfonts")
	r.Static("/uploads", "./uploads")
	r.StaticFile("/login", "./template/login.html")

	// favicon 处理
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	// 公开路由无需鉴权
	r.POST("/login", handlers.LoginPostHandler)
	r.GET("/logout", handlers.LogoutHandler)

	// 主页面路由
	authorized := r.Group("/")
	authorized.Use(middleware.AuthMiddleware())
	{
		// 根路径：根据角色分流
		authorized.GET("/", func(c *gin.Context) {
			session := sessions.Default(c)
			role := session.Get("role")
			if role == "admin" {
				c.File("./template/proctor.html")
				return
			}
			c.File("./template/proctor.html")
		})

		admin := authorized.Group("/admin")
		admin.Use(middleware.AdminMiddleware())
		{
			admin.GET("/", func(c *gin.Context) {
				c.File("./template/index.html")
			})
		}
	}

	// API 路由
	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware())
	{
		// 获取个人信息
		api.GET("/me", func(c *gin.Context) {
			session := sessions.Default(c)
			c.JSON(http.StatusOK, gin.H{
				"id":       session.Get("user_id"),
				"username": session.Get("username"),
				"role":     session.Get("role"),
			})
		})

		// 监考员专用 API
		api.GET("/proctor/nodes", handlers.ListNodes)
		api.POST("/proctor/nodes/:id/jump", handlers.GetNodeJumpURL)
		api.POST("/proctor/nodes/:id/release", handlers.ReleaseNode)
		api.PUT("/users/password", handlers.ChangePassword)

		adminAPI := api.Group("/")
		adminAPI.Use(middleware.AdminMiddleware())
		{
			// 用户管理
			adminAPI.GET("/users", handlers.ListUsers)
			adminAPI.GET("/users/:id", handlers.GetUser)
			adminAPI.POST("/users", handlers.CreateUser)
			adminAPI.DELETE("/users/:id", handlers.DeleteUser)
			adminAPI.PUT("/users/:id", handlers.UpdateUser)

			// 教室管理
			adminAPI.GET("/rooms", handlers.ListRooms)
			adminAPI.GET("/rooms/:id", handlers.GetRoom)
			adminAPI.POST("/rooms", handlers.CreateRoom)
			adminAPI.DELETE("/rooms/:id", handlers.DeleteRoom)
			adminAPI.PUT("/rooms/:id", handlers.UpdateRoom)

			// 节点管理
			adminAPI.GET("/nodes", handlers.ListNodes)
			adminAPI.GET("/nodes/stats", handlers.GetNodeStats)
			adminAPI.POST("/nodes", handlers.CreateNode)
			adminAPI.DELETE("/nodes/:id", handlers.DeleteNode)
			adminAPI.PUT("/nodes/:id", handlers.UpdateNode)
			adminAPI.GET("/nodes/:id/jump", handlers.GetNodeJumpURL)
			adminAPI.POST("/nodes/:id/release", handlers.ReleaseNode)

			// 考试管理（完整CRUD）
			adminAPI.GET("/exams", handlers.ListExams)
			adminAPI.GET("/exams/:id", handlers.GetExams)
			adminAPI.POST("/exams", handlers.CreateExam)
			adminAPI.PUT("/exams/:id", handlers.UpdateExam)
			adminAPI.DELETE("/exams/:id", handlers.DeleteExam)
			adminAPI.POST("/exams/:id/end", handlers.EndExam)
			adminAPI.GET("/exams/stats", handlers.GetExamStats)
			adminAPI.POST("/exams/:id/retry-schedule", handlers.RetryAssignAndNotifyExam)

			// 异常管理（完整CRUD）
			adminAPI.GET("/alerts", handlers.ListAlerts)
			adminAPI.GET("/alerts/:id", handlers.GetAlerts)
			adminAPI.POST("/alerts", handlers.CreateAlert)
			adminAPI.PUT("/alerts/:id", handlers.UpdateAlert)
			adminAPI.DELETE("/alerts/:id", handlers.DeleteAlert)

			// 配置同步
			adminAPI.POST("/sync/rooms", handlers.SyncRooms)
		}
	}

	// 边缘节点专用 API
	nodeAPI := r.Group("/node-api/v1")
	nodeAPI.Use(middleware.NodeAuthMiddleware())
	{
		nodeAPI.POST("/heartbeat", handlers.NodeHeartbeat)
		nodeAPI.POST("/tasks/sync", handlers.SyncTask)
		nodeAPI.POST("/alerts", handlers.ReportAlert)
	}

	r.Run(":8080")
}
