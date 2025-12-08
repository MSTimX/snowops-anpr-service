package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"anpr-service/internal/domain/anpr"
	"anpr-service/internal/repository"
	"anpr-service/internal/utils"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("not found")
)

type ANPRService struct {
	repo *repository.ANPRRepository
	log  zerolog.Logger
}

func NewANPRService(repo *repository.ANPRRepository, log zerolog.Logger) *ANPRService {
	return &ANPRService{
		repo: repo,
		log:  log,
	}
}

func (s *ANPRService) ProcessIncomingEvent(ctx context.Context, payload anpr.EventPayload, defaultCameraModel string) (*anpr.ProcessResult, error) {
	if payload.Plate == "" {
		return nil, fmt.Errorf("%w: plate is required", ErrInvalidInput)
	}
	if payload.CameraID == "" {
		return nil, fmt.Errorf("%w: camera_id is required", ErrInvalidInput)
	}
	if payload.EventTime.IsZero() {
		return nil, fmt.Errorf("%w: event_time is required", ErrInvalidInput)
	}

	normalized := utils.NormalizePlate(payload.Plate)
	if normalized == "" {
		return nil, fmt.Errorf("%w: plate cannot be empty after normalization", ErrInvalidInput)
	}

	plateID, err := s.repo.GetOrCreatePlate(ctx, normalized, payload.Plate)
	if err != nil {
		s.log.Error().
			Err(err).
			Str("normalized", normalized).
			Str("original", payload.Plate).
			Msg("failed to get or create plate")
		return nil, fmt.Errorf("failed to get or create plate: %w", err)
	}
	
	s.log.Info().
		Str("plate_id", plateID.String()).
		Str("normalized", normalized).
		Str("original", payload.Plate).
		Msg("plate retrieved or created successfully")

	cameraModel := payload.CameraModel
	if cameraModel == "" {
		cameraModel = defaultCameraModel
	}

	event := &anpr.Event{
		PlateID:         plateID,
		EventPayload:    payload,
		NormalizedPlate: normalized,
	}
	event.CameraModel = cameraModel

	// Извлекаем данные о снеге из RawPayload, если они там есть
	// (для обратной совместимости с данными, которые уже в RawPayload)
	if payload.RawPayload != nil {
		if snowEventTimeStr, ok := payload.RawPayload["snow_event_time"].(string); ok && snowEventTimeStr != "" {
			if snowTime, err := time.Parse(time.RFC3339, snowEventTimeStr); err == nil {
				event.SnowEventTime = &snowTime
			}
		}
		if snowCameraID, ok := payload.RawPayload["snow_camera_id"].(string); ok && snowCameraID != "" {
			event.SnowCameraID = snowCameraID
		}
		if snowVolumePct, ok := payload.RawPayload["snow_volume_percentage"].(float64); ok {
			event.SnowVolumePercentage = &snowVolumePct
		}
		if snowVolumeConf, ok := payload.RawPayload["snow_volume_confidence"].(float64); ok {
			event.SnowVolumeConfidence = &snowVolumeConf
		}
		if snowDirection, ok := payload.RawPayload["snow_direction_ai"].(string); ok && snowDirection != "" {
			event.SnowDirectionAI = snowDirection
		}
		if matchedSnow, ok := payload.RawPayload["matched_snow"].(bool); ok {
			event.MatchedSnow = matchedSnow
		}
	}

	if err := s.repo.CreateANPREvent(ctx, event); err != nil {
		s.log.Error().
			Err(err).
			Str("plate", normalized).
			Str("camera_id", payload.CameraID).
			Msg("failed to create ANPR event")
		return nil, fmt.Errorf("failed to create ANPR event: %w", err)
	}

	s.log.Info().
		Str("event_id", event.ID.String()).
		Str("plate_id", plateID.String()).
		Str("plate", normalized).
		Str("raw_plate", payload.Plate).
		Str("camera_id", payload.CameraID).
		Time("event_time", payload.EventTime).
		Msg("saved ANPR event to database")

	hits, err := s.repo.FindListsForPlate(ctx, plateID)
	if err != nil {
		s.log.Error().
			Err(err).
			Str("plate_id", plateID.String()).
			Msg("failed to find lists for plate")
		return nil, fmt.Errorf("failed to find lists for plate: %w", err)
	}

	if len(hits) > 0 {
		s.log.Info().
			Str("plate_id", plateID.String()).
			Str("plate", normalized).
			Int("hits_count", len(hits)).
			Msg("plate found in lists")
		for _, hit := range hits {
			s.log.Debug().
				Str("list_id", hit.ListID.String()).
				Str("list_name", hit.ListName).
				Str("list_type", hit.ListType).
				Msg("list hit")
		}
	} else {
		s.log.Debug().
			Str("plate_id", plateID.String()).
			Str("plate", normalized).
			Msg("plate not found in any lists")
	}

	return &anpr.ProcessResult{
		EventID: event.ID,
		PlateID: plateID,
		Plate:   normalized,
		Hits:    hits,
	}, nil
}

func (s *ANPRService) FindPlates(ctx context.Context, plateQuery string) ([]PlateInfo, error) {
	normalized := utils.NormalizePlate(plateQuery)
	if normalized == "" {
		return nil, fmt.Errorf("%w: plate query cannot be empty", ErrInvalidInput)
	}

	plates, err := s.repo.FindPlatesByNormalized(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to find plates: %w", err)
	}

	result := make([]PlateInfo, 0, len(plates))
	for _, p := range plates {
		lastEventTime, _ := s.repo.GetLastEventTimeForPlate(ctx, p.ID)
		info := PlateInfo{
			ID:            p.ID.String(),
			Number:        p.Number,
			Normalized:    p.Normalized,
			LastEventTime: lastEventTime,
		}
		result = append(result, info)
	}

	return result, nil
}

func (s *ANPRService) FindEvents(ctx context.Context, plateQuery *string, from, to *string, limit, offset int) ([]EventInfo, error) {
	var normalizedPlate *string
	if plateQuery != nil {
		normalized := utils.NormalizePlate(*plateQuery)
		if normalized != "" {
			normalizedPlate = &normalized
		}
	}

	var fromTime, toTime *time.Time
	if from != nil && *from != "" {
		t, err := time.Parse(time.RFC3339, *from)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid from time format", ErrInvalidInput)
		}
		fromTime = &t
	}
	if to != nil && *to != "" {
		t, err := time.Parse(time.RFC3339, *to)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid to time format", ErrInvalidInput)
		}
		toTime = &t
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	events, err := s.repo.FindEvents(ctx, normalizedPlate, fromTime, toTime, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to find events: %w", err)
	}

	result := make([]EventInfo, 0, len(events))
	for _, e := range events {
		var plateID *string
		if e.PlateID != nil {
			id := e.PlateID.String()
			plateID = &id
		}
		info := EventInfo{
			ID:                e.ID.String(),
			PlateID:           plateID,
			CameraID:          e.CameraID,
			CameraModel:       e.CameraModel,
			Direction:         e.Direction,
			Lane:              e.Lane,
			RawPlate:          e.RawPlate,
			NormalizedPlate:   e.NormalizedPlate,
			Confidence:        e.Confidence,
			VehicleColor:      e.VehicleColor,
			VehicleType:       e.VehicleType,
			VehicleBrand:      e.VehicleBrand,
			VehicleModel:      e.VehicleModel,
			VehicleCountry:    e.VehicleCountry,
			VehiclePlateColor: e.VehiclePlateColor,
			VehicleSpeed:      e.VehicleSpeed,
			SnapshotURL:       e.SnapshotURL,
			EventTime:         e.EventTime,
		}
		result = append(result, info)
	}

	return result, nil
}

// CleanupOldEvents удаляет события старше указанного количества дней
func (s *ANPRService) CleanupOldEvents(ctx context.Context, days int) (int64, error) {
	deleted, err := s.repo.DeleteOldEvents(ctx, days)
	if err != nil {
		s.log.Error().Err(err).Int("days", days).Msg("failed to cleanup old events")
		return 0, err
	}
	if deleted > 0 {
		s.log.Info().Int64("deleted_count", deleted).Int("days", days).Msg("cleaned up old events")
	}
	return deleted, nil
}

// SyncVehicleToWhitelist синхронизирует номер транспортного средства в whitelist
// Вызывается при создании/обновлении vehicle в roles сервисе
func (s *ANPRService) SyncVehicleToWhitelist(ctx context.Context, plateNumber string) (uuid.UUID, error) {
	plateID, err := s.repo.SyncVehicleToWhitelist(ctx, plateNumber)
	if err != nil {
		s.log.Error().Err(err).Str("plate_number", plateNumber).Msg("failed to sync vehicle to whitelist")
		return uuid.Nil, fmt.Errorf("sync vehicle to whitelist: %w", err)
	}

	s.log.Info().
		Str("plate_number", plateNumber).
		Str("plate_id", plateID.String()).
		Msg("vehicle synced to whitelist")

	return plateID, nil
}

type PlateInfo struct {
	ID            string     `json:"id"`
	Number        string     `json:"number"`
	Normalized    string     `json:"normalized"`
	LastEventTime *time.Time `json:"last_event_time,omitempty"`
}

type EventInfo struct {
	ID                string    `json:"id"`
	PlateID           *string   `json:"plate_id,omitempty"`
	CameraID          string    `json:"camera_id"`
	CameraModel       *string   `json:"camera_model,omitempty"`
	Direction         *string   `json:"direction,omitempty"`
	Lane              *int      `json:"lane,omitempty"`
	RawPlate          string    `json:"raw_plate"`
	NormalizedPlate   string    `json:"normalized_plate"`
	Confidence        *float64  `json:"confidence,omitempty"`
	VehicleColor      *string   `json:"vehicle_color,omitempty"`
	VehicleType       *string   `json:"vehicle_type,omitempty"`
	VehicleBrand      *string   `json:"vehicle_brand,omitempty"`
	VehicleModel      *string   `json:"vehicle_model,omitempty"`
	VehicleCountry    *string   `json:"vehicle_country,omitempty"`
	VehiclePlateColor *string   `json:"vehicle_plate_color,omitempty"`
	VehicleSpeed      *float64  `json:"vehicle_speed,omitempty"`
	SnapshotURL       *string   `json:"snapshot_url,omitempty"`
	EventTime         time.Time `json:"event_time"`
}
