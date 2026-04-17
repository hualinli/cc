package handlers

import (
	"cc/models"
	"time"
)

type userPayload struct {
	ID        uint      `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type roomPayload struct {
	ID        uint      `json:"id"`
	Building  string    `json:"building"`
	Name      string    `json:"name"`
	Type      *string   `json:"type,omitempty"`
	Remark    *string   `json:"remark,omitempty"`
	RTSPUrl   string    `json:"rtsp_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type nodePayload struct {
	ID                    uint       `json:"id"`
	Name                  string     `json:"name"`
	Token                 string     `json:"token"`
	NodeModel             string     `json:"nodemodel"`
	Address               string     `json:"address"`
	Status                string     `json:"status"`
	Version               string     `json:"version"`
	ConfigVersion         int        `json:"config_version"`
	CurrentUserID         *uint      `json:"current_user_id"`
	CurrentUserOccupiedAt *time.Time `json:"current_user_occupied_at"`
	CurrentExamID         *uint      `json:"current_exam_id"`
	LastHeartbeatAt       time.Time  `json:"last_heartbeat_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type examPayload struct {
	ID              uint         `json:"id"`
	Name            string       `json:"name"`
	Subject         string       `json:"subject"`
	RoomID          uint         `json:"room_id"`
	NodeID          *uint        `json:"node_id"`
	UserID          uint         `json:"user_id"`
	DurationSeconds int          `json:"duration_seconds"`
	StartTime       time.Time    `json:"start_time"`
	EndTime         *time.Time   `json:"end_time"`
	ScheduleStatus  string       `json:"schedule_status"`
	ScheduleError   string       `json:"schedule_error,omitempty"`
	Remark          string       `json:"remark,omitempty"`
	ExamineeCount   int          `json:"examinee_count"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	Room            *roomPayload `json:"room,omitempty"`
	Node            *nodePayload `json:"node,omitempty"`
	User            *userPayload `json:"user,omitempty"`
}

func toUserPayload(u models.User) userPayload {
	return userPayload{
		ID:        u.ID,
		Username:  u.Username,
		Role:      string(u.Role),
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func toRoomPayload(r models.Room) roomPayload {
	return roomPayload{
		ID:        r.ID,
		Building:  r.Building,
		Name:      r.Name,
		Type:      r.Type,
		Remark:    r.Remark,
		RTSPUrl:   r.RTSPUrl,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toNodePayload(n models.Node) nodePayload {
	return nodePayload{
		ID:                    n.ID,
		Name:                  n.Name,
		Token:                 n.Token,
		NodeModel:             n.NodeModel,
		Address:               n.Address,
		Status:                n.Status,
		Version:               n.Version,
		ConfigVersion:         n.ConfigVersion,
		CurrentUserID:         n.CurrentUserID,
		CurrentUserOccupiedAt: n.CurrentUserOccupiedAt,
		CurrentExamID:         n.CurrentExamID,
		LastHeartbeatAt:       n.LastHeartbeatAt,
		CreatedAt:             n.CreatedAt,
		UpdatedAt:             n.UpdatedAt,
	}
}

func toExamPayload(e models.Exam) examPayload {
	payload := examPayload{
		ID:              e.ID,
		Name:            e.Name,
		Subject:         e.Subject,
		RoomID:          e.RoomID,
		NodeID:          e.NodeID,
		UserID:          e.UserID,
		DurationSeconds: e.DurationSeconds,
		StartTime:       e.StartTime,
		EndTime:         e.EndTime,
		ScheduleStatus:  e.ScheduleStatus,
		ScheduleError:   e.ScheduleError,
		Remark:          e.Remark,
		ExamineeCount:   e.ExamineeCount,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}

	if e.Room != nil {
		room := toRoomPayload(*e.Room)
		payload.Room = &room
	}
	if e.Node != nil {
		node := toNodePayload(*e.Node)
		payload.Node = &node
	}
	if e.User != nil {
		user := toUserPayload(*e.User)
		payload.User = &user
	}

	return payload
}