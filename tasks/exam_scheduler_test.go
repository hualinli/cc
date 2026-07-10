package tasks

import (
	"cc/models"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSchedulerTestDB(t *testing.T) func() {
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

func seedSchedulerUser(t *testing.T) models.User {
	user := models.User{Username: "scheduler-user", Password: "hashed", Role: "admin", Status: "active"}
	if err := models.DB.Create(&user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

func seedSchedulerRoom(t *testing.T) models.Room {
	room := models.Room{Name: "scheduler-room", Building: "A", RTSPUrl: "rtsp://example.com/test"}
	if err := models.DB.Create(&room).Error; err != nil {
		t.Fatalf("failed to seed room: %v", err)
	}
	return room
}

func seedSchedulerNode(t *testing.T, name, status string) models.Node {
	node := models.Node{
		Name:            name,
		Token:           "test-token-" + name,
		NodeModel:       "Standard-v1",
		Address:         "192.168.1.100:8002",
		Status:          status,
		LastHeartbeatAt: time.Now(),
	}
	if err := models.DB.Create(&node).Error; err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	return node
}

// TestPickAvailableNode 测试节点选择逻辑
func TestPickAvailableNode(t *testing.T) {
	cleanup := setupSchedulerTestDB(t)
	defer cleanup()

	t.Run("has_available_node", func(t *testing.T) {
		node := seedSchedulerNode(t, "available-node", models.NodeStatusIdle)

		var pickedNode models.Node
		var found bool
		var err error

		models.DB.Transaction(func(tx *gorm.DB) error {
			pickedNode, found, err = pickAvailableNode(tx)
			return err
		})

		if err != nil {
			t.Fatalf("pickAvailableNode failed: %v", err)
		}
		if !found {
			t.Fatal("expected to find available node")
		}
		if pickedNode.ID != node.ID {
			t.Errorf("expected node %d, got %d", node.ID, pickedNode.ID)
		}
	})

	t.Run("no_available_node", func(t *testing.T) {
		cleanup := setupSchedulerTestDB(t)
		defer cleanup()

		// 所有节点都忙碌
		seedSchedulerNode(t, "busy-node-1", models.NodeStatusBusy)
		seedSchedulerNode(t, "busy-node-2", models.NodeStatusBusy)

		var found bool
		var err error

		models.DB.Transaction(func(tx *gorm.DB) error {
			_, found, err = pickAvailableNode(tx)
			return err
		})

		if err != nil {
			t.Fatalf("pickAvailableNode failed: %v", err)
		}
		if found {
			t.Error("expected no available node")
		}
	})

	t.Run("node_heartbeat_timeout", func(t *testing.T) {
		cleanup := setupSchedulerTestDB(t)
		defer cleanup()

		// 节点心跳超时
		node := seedSchedulerNode(t, "timeout-node", models.NodeStatusIdle)
		models.DB.Model(&node).Update("last_heartbeat_at", time.Now().Add(-5*time.Minute))

		var found bool
		var err error

		models.DB.Transaction(func(tx *gorm.DB) error {
			_, found, err = pickAvailableNode(tx)
			return err
		})

		if err != nil {
			t.Fatalf("pickAvailableNode failed: %v", err)
		}
		if found {
			t.Error("expected no available node (heartbeat timeout)")
		}
	})

	t.Run("node_already_occupied", func(t *testing.T) {
		cleanup := setupSchedulerTestDB(t)
		defer cleanup()

		user := seedSchedulerUser(t)
		room := seedSchedulerRoom(t)
		node := seedSchedulerNode(t, "occupied-node", models.NodeStatusIdle)

		// 创建进行中的考试
		exam := models.Exam{
			Name:            "running-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			NodeID:          &node.ID,
			StartTime:       time.Now(),
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamScheduleRunning,
		}
		models.DB.Create(&exam)

		// 更新节点状态
		models.DB.Model(&node).Updates(map[string]any{
			"status":          models.NodeStatusBusy,
			"current_exam_id": exam.ID,
		})

		var found bool
		var err error

		models.DB.Transaction(func(tx *gorm.DB) error {
			_, found, err = pickAvailableNode(tx)
			return err
		})

		if err != nil {
			t.Fatalf("pickAvailableNode failed: %v", err)
		}
		if found {
			t.Error("expected no available node (already occupied)")
		}
	})
}

// TestLockAvailableNode 测试节点锁定逻辑
func TestLockAvailableNode(t *testing.T) {
	t.Run("lock_success", func(t *testing.T) {
		cleanup := setupSchedulerTestDB(t)
		defer cleanup()

		user := seedSchedulerUser(t)
		room := seedSchedulerRoom(t)
		seedSchedulerNode(t, "lock-node", models.NodeStatusIdle)

		exam := models.Exam{
			Name:            "test-exam",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			StartTime:       time.Now(),
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamSchedulePending,
		}

		var lockedNode models.Node
		var found bool
		var err error

		models.DB.Transaction(func(tx *gorm.DB) error {
			lockedNode, found, err = lockAvailableNodeForExam(tx, exam)
			return err
		})

		if err != nil {
			t.Fatalf("lockAvailableNodeForExam failed: %v", err)
		}
		if !found {
			t.Fatal("expected to lock a node")
		}

		// 验证节点状态
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, lockedNode.ID)

		if reloadedNode.Status != models.NodeStatusBusy {
			t.Errorf("expected status busy, got %s", reloadedNode.Status)
		}
		if reloadedNode.CurrentExamID == nil {
			t.Error("expected current_exam_id to be set")
		}
	})

	t.Run("concurrent_lock_conflict", func(t *testing.T) {
		cleanup := setupSchedulerTestDB(t)
		defer cleanup()

		user := seedSchedulerUser(t)
		room := seedSchedulerRoom(t)
		node := seedSchedulerNode(t, "conflict-node", models.NodeStatusIdle)

		exam1 := models.Exam{
			Name:            "exam-1",
			Subject:         "math",
			RoomID:          room.ID,
			UserID:          user.ID,
			StartTime:       time.Now(),
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamSchedulePending,
		}
		exam2 := models.Exam{
			Name:            "exam-2",
			Subject:         "english",
			RoomID:          room.ID,
			UserID:          user.ID,
			StartTime:       time.Now(),
			DurationSeconds: 3600,
			ScheduleStatus:  models.ExamSchedulePending,
		}

		// 第一个考试锁定节点
		models.DB.Transaction(func(tx *gorm.DB) error {
			_, found, err := lockAvailableNodeForExam(tx, exam1)
			if err != nil {
				return err
			}
			if !found {
				t.Fatal("expected to lock node for exam1")
			}
			return nil
		})

		// 第二个考试尝试锁定同一节点（应该失败）
		var found bool
		models.DB.Transaction(func(tx *gorm.DB) error {
			_, found, _ = lockAvailableNodeForExam(tx, exam2)
			return nil
		})

		if found {
			t.Error("expected second lock to fail (node already locked)")
		}

		// 验证节点仍然属于第一个考试
		var reloadedNode models.Node
		models.DB.First(&reloadedNode, node.ID)

		if reloadedNode.CurrentExamID == nil {
			t.Fatal("expected current_exam_id to be set")
		}
		// 注意：这里我们无法直接验证是 exam1，因为 exam1 还没创建到数据库
		// 在实际场景中，exam 会先创建再锁定节点
	})
}

// TestUnlockNodeForExam 测试节点释放逻辑
func TestUnlockNodeForExam(t *testing.T) {
	cleanup := setupSchedulerTestDB(t)
	defer cleanup()

	user := seedSchedulerUser(t)
	room := seedSchedulerRoom(t)
	node := seedSchedulerNode(t, "unlock-node", models.NodeStatusBusy)

	exam := models.Exam{
		Name:            "unlock-exam",
		Subject:         "math",
		RoomID:          room.ID,
		UserID:          user.ID,
		NodeID:          &node.ID,
		StartTime:       time.Now(),
		DurationSeconds: 3600,
		ScheduleStatus:  models.ExamScheduleRunning,
	}
	models.DB.Create(&exam)

	// 设置节点为忙碌状态
	models.DB.Model(&node).Updates(map[string]any{
		"status":          models.NodeStatusBusy,
		"current_exam_id": exam.ID,
	})

	// 释放节点
	unlockNodeForExam(node.ID, exam.ID, "test")

	// 验证节点状态
	var reloadedNode models.Node
	models.DB.First(&reloadedNode, node.ID)

	if reloadedNode.Status != models.NodeStatusIdle {
		t.Errorf("expected status idle, got %s", reloadedNode.Status)
	}
	if reloadedNode.CurrentExamID != nil {
		t.Error("expected current_exam_id to be nil")
	}
}
