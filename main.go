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

	// 调试/初始化专用：节点配置一键导出 (正式上线前务必注释掉或删掉此行)
	r.GET("/api/nodes/:id/export", handlers.ExportNodeConfig)

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

	// 细粒度 API
	// 注意：所有 API 均需鉴权, 但调试期间暂时开放

	developAPI := r.Group("/dev-api/v1")
	{
		// 1. Node API 的增删改查
		developAPI.GET("/nodes/:id", handlers.GetNode)
		developAPI.POST("/nodes", handlers.CreateNode)
		developAPI.DELETE("/nodes/:id", handlers.DeleteNode)
		developAPI.PUT("/nodes/:id", handlers.UpdateNode)
		developAPI.GET("/nodes", handlers.ListNodes)

		// 2. Room API 的增删改查
		developAPI.GET("/rooms/:id", handlers.GetRoom)
		developAPI.POST("/rooms", handlers.CreateRoom)
		developAPI.DELETE("/rooms/:id", handlers.DeleteRoom)
		developAPI.PUT("/rooms/:id", handlers.UpdateRoom)
		developAPI.GET("/rooms", handlers.ListRooms)

		// 3. Exam API 的增删改查
		developAPI.GET("/exams/:id", handlers.GetExams)
		developAPI.POST("/exams", handlers.CreateExam)
		developAPI.DELETE("/exams/:id", handlers.DeleteExam)
		developAPI.PUT("/exams/:id", handlers.UpdateExam)
		developAPI.GET("/exams", handlers.ListExams)

		// 4. Alert API 的增删改查
		developAPI.GET("/alerts/:id", handlers.GetAlerts)
		developAPI.POST("/alerts", handlers.CreateAlert)
		developAPI.DELETE("/alerts/:id", handlers.DeleteAlert)
		developAPI.PUT("/alerts/:id", handlers.UpdateAlert)
		developAPI.GET("/alerts", handlers.ListAlerts)
	}

	// 业务 API
	// 注意：所有 API 均需鉴权，但调试期间暂时开放
	// TODO

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
			adminAPI.POST("/users", handlers.CreateUser)
			adminAPI.DELETE("/users/:id", handlers.DeleteUser)
			adminAPI.PUT("/users/:id", handlers.UpdateUser)

			// 节点管理
			adminAPI.GET("/nodes", handlers.ListNodes)
			adminAPI.POST("/nodes", handlers.CreateNode)
			adminAPI.DELETE("/nodes/:id", handlers.DeleteNode)
			adminAPI.PUT("/nodes/:id", handlers.UpdateNode)
			adminAPI.GET("/nodes/:id/jump", handlers.GetNodeJumpURL)
			adminAPI.POST("/nodes/:id/release", handlers.ReleaseNode)

			// 教室管理
			adminAPI.GET("/rooms", handlers.ListRooms)
			adminAPI.POST("/rooms", handlers.CreateRoom)
			adminAPI.DELETE("/rooms/:id", handlers.DeleteRoom)
			adminAPI.PUT("/rooms/:id", handlers.UpdateRoom)

			// 考试管理（完整CRUD）
			adminAPI.GET("/exams", handlers.ListExams)
			adminAPI.GET("/exams/:id", handlers.GetExams)
			adminAPI.GET("/exams/stats", handlers.GetExamStats)
			adminAPI.POST("/exams", handlers.CreateExam)
			adminAPI.PUT("/exams/:id", handlers.UpdateExam)
			adminAPI.DELETE("/exams/:id", handlers.DeleteExam)

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
