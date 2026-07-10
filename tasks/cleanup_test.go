package tasks

import (
	"cc/models"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCleanupTestDB(t *testing.T) func() {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	models.DB = db

	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}, &models.Alert{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	return func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	}
}

func seedCleanupUser(t *testing.T) models.User {
	user := models.User{Username: "cleanup-user", Password: "hashed", Role: "admin", Status: "active"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedCleanupRoom(t *testing.T) models.Room {
	room := models.Room{Name: "cleanup-room", Building: "A", RTSPUrl: "rtsp://test"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedCleanupNode(t *testing.T, name, status string, lastHeartbeatAt, leaseExpiresAt time.Time) models.Node {
	node := models.Node{
		Name:            name,
		Token:           "token-" + name,
		NodeModel:       "Standard",
		Address:         "192.168.1.100:8002",
		Status:          status,
		LastHeartbeatAt: lastHeartbeatAt,
		LeaseExpiresAt:  leaseExpiresAt,
	}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

// TestCheckExpiredLeases 测试租约过期检测
func TestCheckExpiredLeases(t *testing.T) {
	t.Run("lease_expired_with_running_exam", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)

		// 绕过启动宽限期
		cleanupStartedAt = time.Now().Add(-5 * time.Minute)

		// 创建租约和心跳均已过期的节点（双重条件）
		node := seedCleanupNode(t, "expired-node", models.NodeStatusBusy,
			time.Now().Add(-3*time.Minute),  // last_heartbeat_at 过期
			time.Now().Add(-1*time.Minute))  // lease_expires_at 过期

		// 创建进行中的考试
		exam := models.Exam{
			Name:            "running-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			NodeID:          &node.ID,
			StartTime:       time.Now().Add(-30 * time.Minute),
			DurationSeconds: 7200,
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)

		// 更新节点关联考试
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		checkExpiredLeases()

		// 验证考试已关闭
		var reloadedExam models.Exam
		models.DB.First(&reloadedExam, exam.ID)

		if reloadedExam.EndTime == nil {
			t.Error("expected exam to be closed")
		}
		if reloadedExam.ScheduleStatus != models.ExamScheduleInterrupted {
			t.Errorf("expected status interrupted, got %s", reloadedExam.ScheduleStatus)
		}

		// 验证节点已释放
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID != nil {
			t.Error("expected current_exam_id to be nil")
		}
		if reloadedNode.Status != models.NodeStatusOffline {
			t.Errorf("expected status offline, got %s", reloadedNode.Status)
		}

		// 验证创建了告警
		var alertCount int64
		models.DB.Model(&models.Alert{}).Where("exam_id = ? AND type = ?", exam.ID, "node_lease_expired").Count(&alertCount)
		if alertCount != 1 {
			t.Errorf("expected 1 alert, got %d", alertCount)
		}
	})

	t.Run("lease_not_expired", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)

		// 创建租约未过期的节点
		node := seedCleanupNode(t, "active-node", models.NodeStatusBusy, time.Now(), time.Now().Add(1*time.Minute))

		// 创建进行中的考试
		exam := models.Exam{
			Name:            "running-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			NodeID:          &node.ID,
			StartTime:       time.Now().Add(-30 * time.Minute),
			DurationSeconds: 7200,
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		checkExpiredLeases()

		// 验证考试未关闭
		var reloadedExam models.Exam
		models.DB.First(&reloadedExam, exam.ID)

		if reloadedExam.EndTime != nil {
			t.Error("expected exam to still be running")
		}

		// 验证节点未变化
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID == nil || *reloadedNode.CurrentExamID != exam.ID {
			t.Error("expected current_exam_id to remain set")
		}
	})
}

// TestCloseExpiredExams 测试考试超时自动关闭
func TestCloseExpiredExams(t *testing.T) {
	t.Run("exam_expired_beyond_grace_period", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)
		node := seedCleanupNode(t, "test-node", models.NodeStatusBusy, time.Now(), time.Now().Add(1*time.Minute))

		// 创建超时的考试（开始于 2 小时前，时长 1 小时）
		exam := models.Exam{
			Name:            "expired-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			NodeID:          &node.ID,
			StartTime:       time.Now().Add(-2 * time.Hour),
			DurationSeconds: 3600, // 1 小时
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		closeExpiredExams()

		// 验证考试已关闭
		var reloadedExam models.Exam
		models.DB.First(&reloadedExam, exam.ID)

		if reloadedExam.EndTime == nil {
			t.Error("expected exam to be closed")
		}
		if reloadedExam.ScheduleStatus != models.ExamScheduleAutoClosed {
			t.Errorf("expected status auto_closed, got %s", reloadedExam.ScheduleStatus)
		}

		// 验证节点已释放
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID != nil {
			t.Error("expected current_exam_id to be nil")
		}
		if reloadedNode.Status != models.NodeStatusIdle {
			t.Errorf("expected status idle, got %s", reloadedNode.Status)
		}
	})

	t.Run("exam_within_grace_period", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)
		node := seedCleanupNode(t, "test-node", models.NodeStatusBusy, time.Now(), time.Now().Add(1*time.Minute))

		// 创建刚超过时长的考试（在宽限期内）
		exam := models.Exam{
			Name:            "recent-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			NodeID:          &node.ID,
			StartTime:       time.Now().Add(-65 * time.Minute),
			DurationSeconds: 3600, // 1 小时
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		closeExpiredExams()

		// 验证考试未关闭（仍在宽限期内）
		var reloadedExam models.Exam
		models.DB.First(&reloadedExam, exam.ID)

		if reloadedExam.EndTime != nil {
			t.Error("expected exam to still be running (within grace period)")
		}
	})
}

// TestCleanOrphanData 测试清理孤儿数据
func TestCleanOrphanData(t *testing.T) {
	t.Run("idle_node_with_current_exam_id", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)
		node := seedCleanupNode(t, "idle-node", models.NodeStatusIdle, time.Now(), time.Now().Add(1*time.Minute))

		// 创建考试
		exam := models.Exam{
			Name:            "test-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			StartTime:       time.Now(),
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)

		// 节点是 idle 但有 current_exam_id（孤儿数据）
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		cleanOrphanData()

		// 验证 current_exam_id 已清空
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID != nil {
			t.Error("expected current_exam_id to be cleared")
		}
	})

	t.Run("node_pointing_to_ended_exam", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		user := seedCleanupUser(t)
		room := seedCleanupRoom(t)
		node := seedCleanupNode(t, "test-node", models.NodeStatusBusy, time.Now(), time.Now().Add(1*time.Minute))

		// 创建已结束的考试
		endTime := time.Now().Add(-5 * time.Minute)
		exam := models.Exam{
			Name:            "ended-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			StartTime:       time.Now().Add(-2 * time.Hour),
			EndTime:         &endTime,
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)

		// 节点仍指向已结束的考试
		models.DB.Model(&node).Update("current_exam_id", exam.ID)

		// 运行清理
		cleanOrphanData()

		// 验证 current_exam_id 已清空
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID != nil {
			t.Error("expected current_exam_id to be cleared")
		}
	})
}

// TestMarkOfflineNodes 测试标记离线节点
func TestMarkOfflineNodes(t *testing.T) {
	t.Run("heartbeat_timeout", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		// 创建心跳超时的节点
		node := seedCleanupNode(t, "timeout-node", models.NodeStatusIdle, time.Now(), time.Now().Add(1*time.Minute))
		models.DB.Model(&node).Update("last_heartbeat_at", time.Now().Add(-10*time.Minute))

		// 运行清理
		markOfflineNodes()

		// 验证节点已标记为离线
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.Status != models.NodeStatusOffline {
			t.Errorf("expected status offline, got %s", reloadedNode.Status)
		}
	})

	t.Run("heartbeat_active", func(t *testing.T) {
		cleanup := setupCleanupTestDB(t)
		defer cleanup()

		// 创建心跳正常的节点
		node := seedCleanupNode(t, "active-node", models.NodeStatusIdle, time.Now(), time.Now().Add(1*time.Minute))

		// 运行清理
		markOfflineNodes()

		// 验证节点未变化
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.Status != models.NodeStatusIdle {
			t.Errorf("expected status idle, got %s", reloadedNode.Status)
		}
	})
}
