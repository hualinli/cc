package handlers

import (
	"cc/models"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAssociationsTestDB(t *testing.T) func() {
	t.Helper()

	oldDB := models.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.Room{}, &models.Node{}, &models.Exam{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	models.DB = db

	return func() {
		models.DB = oldDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	}
}

func TestLoadExamAssociations(t *testing.T) {
	cleanup := setupAssociationsTestDB(t)
	defer cleanup()

	user := models.User{Username: "testuser", Password: "pass", Role: models.Admin}
	models.DB.Create(&user)

	room := models.Room{Name: "Room-101", Building: "Main", RTSPUrl: "rtsp://test"}
	models.DB.Create(&room)

	node := models.Node{Name: "Node-1", Token: "token123", Status: models.NodeStatusIdle}
	models.DB.Create(&node)

	nodeID := node.ID
	exam := models.Exam{
		Name:      "Test Exam",
		Subject:   "Math",
		RoomID:    room.ID,
		NodeID:    &nodeID,
		UserID:    user.ID,
		StartTime: models.DB.NowFunc(),
	}
	models.DB.Create(&exam)

	t.Run("load all associations", func(t *testing.T) {
		var loadedExam models.Exam
		models.DB.First(&loadedExam, exam.ID)

		if loadedExam.Room != nil {
			t.Error("Room should be nil before loading")
		}
		if loadedExam.Node != nil {
			t.Error("Node should be nil before loading")
		}
		if loadedExam.User != nil {
			t.Error("User should be nil before loading")
		}

		loadExamAssociations(&loadedExam, true, true, true)

		if loadedExam.Room == nil {
			t.Fatal("Room should not be nil after loading")
		}
		if loadedExam.Room.ID != room.ID {
			t.Errorf("expected room ID %d, got %d", room.ID, loadedExam.Room.ID)
		}

		if loadedExam.Node == nil {
			t.Fatal("Node should not be nil after loading")
		}
		if loadedExam.Node.ID != node.ID {
			t.Errorf("expected node ID %d, got %d", node.ID, loadedExam.Node.ID)
		}

		if loadedExam.User == nil {
			t.Fatal("User should not be nil after loading")
		}
		if loadedExam.User.ID != user.ID {
			t.Errorf("expected user ID %d, got %d", user.ID, loadedExam.User.ID)
		}
	})

	t.Run("load only room", func(t *testing.T) {
		var loadedExam models.Exam
		models.DB.First(&loadedExam, exam.ID)

		loadExamAssociations(&loadedExam, true, false, false)

		if loadedExam.Room == nil {
			t.Error("Room should be loaded")
		}
		if loadedExam.Node != nil {
			t.Error("Node should not be loaded")
		}
		if loadedExam.User != nil {
			t.Error("User should not be loaded")
		}
	})

	t.Run("all flags false loads nothing", func(t *testing.T) {
		var loadedExam models.Exam
		models.DB.First(&loadedExam, exam.ID)

		loadExamAssociations(&loadedExam, false, false, false)

		if loadedExam.Room != nil {
			t.Error("Room should not be loaded")
		}
		if loadedExam.Node != nil {
			t.Error("Node should not be loaded")
		}
		if loadedExam.User != nil {
			t.Error("User should not be loaded")
		}
	})

	t.Run("missing room does not panic", func(t *testing.T) {
		examNoRoom := models.Exam{
			Name:      "NoRoom",
			Subject:   "X",
			RoomID:    99999, // 不存在的教室
			UserID:    user.ID,
			StartTime: models.DB.NowFunc(),
		}
		models.DB.Create(&examNoRoom)

		var loaded models.Exam
		models.DB.First(&loaded, examNoRoom.ID)

		// 不应该 panic
		loadExamAssociations(&loaded, true, true, true)
		if loaded.Room != nil {
			t.Error("Room should remain nil for missing room")
		}
	})

	t.Run("missing node does not panic", func(t *testing.T) {
		badNodeID := uint(99999)
		examNoNode := models.Exam{
			Name:      "NoNode",
			Subject:   "X",
			RoomID:    room.ID,
			NodeID:    &badNodeID,
			UserID:    user.ID,
			StartTime: models.DB.NowFunc(),
		}
		models.DB.Create(&examNoNode)

		var loaded models.Exam
		models.DB.First(&loaded, examNoNode.ID)

		loadExamAssociations(&loaded, true, true, true)
		if loaded.Node != nil {
			t.Error("Node should remain nil for missing node")
		}
	})
}

func TestLoadNodeCurrentExam(t *testing.T) {
	cleanup := setupAssociationsTestDB(t)
	defer cleanup()

	user := models.User{Username: "testuser", Password: "pass", Role: models.Admin}
	models.DB.Create(&user)

	room := models.Room{Name: "Room-101", Building: "Main", RTSPUrl: "rtsp://test"}
	models.DB.Create(&room)

	node := models.Node{Name: "Node-1", Token: "token123", Status: models.NodeStatusBusy}
	models.DB.Create(&node)

	nodeID := node.ID
	exam := models.Exam{
		Name:      "Test Exam",
		Subject:   "Math",
		RoomID:    room.ID,
		NodeID:    &nodeID,
		UserID:    user.ID,
		StartTime: models.DB.NowFunc(),
		EndTime:   nil,
	}
	models.DB.Create(&exam)

	models.DB.Model(&node).Update("current_exam_id", exam.ID)

	t.Run("load current exam with room", func(t *testing.T) {
		var loadedNode models.Node
		models.DB.First(&loadedNode, node.ID)

		if loadedNode.CurrentExam != nil {
			t.Error("CurrentExam should be nil before loading")
		}

		loadNodeCurrentExam(&loadedNode, true)

		if loadedNode.CurrentExam == nil {
			t.Fatal("CurrentExam should not be nil after loading")
		}
		if loadedNode.CurrentExam.ID != exam.ID {
			t.Errorf("expected exam ID %d, got %d", exam.ID, loadedNode.CurrentExam.ID)
		}

		if loadedNode.CurrentExam.Room == nil {
			t.Error("CurrentExam.Room should be loaded")
		}
		if loadedNode.CurrentExam.Room.ID != room.ID {
			t.Errorf("expected room ID %d, got %d", room.ID, loadedNode.CurrentExam.Room.ID)
		}
	})

	t.Run("skip offline node", func(t *testing.T) {
		offlineNode := models.Node{Name: "Node-2", Token: "token456", Status: models.NodeStatusOffline}
		models.DB.Create(&offlineNode)

		offlineNodeID := offlineNode.ID
		offlineExam := models.Exam{
			Name:      "Offline Exam",
			Subject:   "Physics",
			RoomID:    room.ID,
			NodeID:    &offlineNodeID,
			UserID:    user.ID,
			StartTime: models.DB.NowFunc(),
			EndTime:   nil,
		}
		models.DB.Create(&offlineExam)
		models.DB.Model(&offlineNode).Update("current_exam_id", offlineExam.ID)

		var loadedNode models.Node
		models.DB.First(&loadedNode, offlineNode.ID)

		loadNodeCurrentExam(&loadedNode, true)

		if loadedNode.CurrentExam != nil {
			t.Error("Offline node should not have CurrentExam")
		}
	})

	t.Run("skip ended exam", func(t *testing.T) {
		endedNode := models.Node{Name: "Node-3", Token: "token789", Status: models.NodeStatusBusy}
		models.DB.Create(&endedNode)

		endedNodeID := endedNode.ID
		endTime := models.DB.NowFunc()
		endedExam := models.Exam{
			Name:      "Ended Exam",
			Subject:   "Chemistry",
			RoomID:    room.ID,
			NodeID:    &endedNodeID,
			UserID:    user.ID,
			StartTime: models.DB.NowFunc(),
			EndTime:   &endTime,
		}
		models.DB.Create(&endedExam)
		models.DB.Model(&endedNode).Update("current_exam_id", endedExam.ID)

		var loadedNode models.Node
		models.DB.First(&loadedNode, endedNode.ID)

		loadNodeCurrentExam(&loadedNode, true)

		if loadedNode.CurrentExam != nil {
			t.Error("Node with ended exam should not have CurrentExam")
		}
	})

	t.Run("nil CurrentExamID is safe", func(t *testing.T) {
		cleanNode := models.Node{Name: "Node-4", Token: "tok4", Status: models.NodeStatusIdle}
		models.DB.Create(&cleanNode)

		var loadedNode models.Node
		models.DB.First(&loadedNode, cleanNode.ID)

		// 不应该 panic
		loadNodeCurrentExam(&loadedNode, false)
		if loadedNode.CurrentExam != nil {
			t.Error("CurrentExam should be nil when CurrentExamID is nil")
		}
	})
}
