package anpr

import (
	"time"
)

type VehicleInfo struct {
	Color string `json:"color,omitempty"`
	Type  string `json:"type,omitempty"`
}

type EventPayload struct {
	CameraID     string                 `json:"camera_id"`
	CameraModel  string                 `json:"camera_model,omitempty"`
	Plate        string                 `json:"plate"`
	Confidence   float64                `json:"confidence"`
	Direction    string                 `json:"direction"`
	Lane         int                    `json:"lane"`
	EventTime    time.Time              `json:"event_time"`
	Vehicle      VehicleInfo            `json:"vehicle"`
	SnapshotURL  string                 `json:"snapshot_url,omitempty"`
	RawPayload   map[string]interface{} `json:"raw_payload,omitempty"`
}

type Event struct {
	ID              int64
	PlateID         int64
	EventPayload
	NormalizedPlate string
}

type ListHit struct {
	ListID   int64  `json:"list_id"`
	ListName string  `json:"list_name"`
	ListType string  `json:"list_type"`
}

type ProcessResult struct {
	EventID int64     `json:"event_id"`
	PlateID int64     `json:"plate_id"`
	Plate   string    `json:"plate"`
	Hits    []ListHit `json:"hits"`
}

