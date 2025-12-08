package anpr

import (
	"time"

	"github.com/google/uuid"
)

type VehicleInfo struct {
	Color      string   `json:"color,omitempty"`
	Type       string   `json:"type,omitempty"`
	Brand      string   `json:"brand,omitempty"`
	Model      string   `json:"model,omitempty"`
	Country    string   `json:"country,omitempty"`
	PlateColor string   `json:"plate_color,omitempty"`
	Speed      *float64 `json:"speed,omitempty"`
}

type EventPayload struct {
	CameraID    string                 `json:"camera_id"`
	CameraModel string                 `json:"camera_model,omitempty"`
	Plate       string                 `json:"plate"`
	Confidence  float64                `json:"confidence"`
	Direction   string                 `json:"direction"`
	Lane        int                    `json:"lane"`
	EventTime   time.Time              `json:"event_time"`
	Vehicle     VehicleInfo            `json:"vehicle"`
	SnapshotURL string                 `json:"snapshot_url,omitempty"`
	RawPayload  map[string]interface{} `json:"raw_payload,omitempty"`
	// Поля для данных о снеге
	SnowEventTime        *time.Time `json:"snow_event_time,omitempty"`
	SnowCameraID         string     `json:"snow_camera_id,omitempty"`
	SnowVolumePercentage *float64   `json:"snow_volume_percentage,omitempty"`
	SnowVolumeConfidence *float64   `json:"snow_volume_confidence,omitempty"`
	SnowDirectionAI      string     `json:"snow_direction_ai,omitempty"`
	MatchedSnow          bool       `json:"matched_snow,omitempty"`
}

type Event struct {
	ID      uuid.UUID
	PlateID uuid.UUID
	EventPayload
	NormalizedPlate string
}

type ListHit struct {
	ListID   uuid.UUID `json:"list_id"`
	ListName string    `json:"list_name"`
	ListType string    `json:"list_type"`
}

type ProcessResult struct {
	EventID uuid.UUID `json:"event_id"`
	PlateID uuid.UUID `json:"plate_id"`
	Plate   string    `json:"plate"`
	Hits    []ListHit `json:"hits"`
}
