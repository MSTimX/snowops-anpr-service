package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"anpr-service/internal/domain/anpr"
	"anpr-service/internal/repository"
	"anpr-service/internal/utils"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound      = errors.New("not found")
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
		s.log.Error().Err(err).Msg("failed to get or create plate")
		return nil, fmt.Errorf("failed to get or create plate: %w", err)
	}

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

	if err := s.repo.CreateANPREvent(ctx, event); err != nil {
		s.log.Error().
			Err(err).
			Str("plate", normalized).
			Str("camera_id", payload.CameraID).
			Msg("failed to create ANPR event")
		return nil, fmt.Errorf("failed to create ANPR event: %w", err)
	}

	s.log.Info().
		Int64("event_id", event.ID).
		Int64("plate_id", plateID).
		Str("plate", normalized).
		Str("raw_plate", payload.Plate).
		Str("camera_id", payload.CameraID).
		Time("event_time", payload.EventTime).
		Msg("saved ANPR event to database")

	hits, err := s.repo.FindListsForPlate(ctx, plateID)
	if err != nil {
		s.log.Error().
			Err(err).
			Int64("plate_id", plateID).
			Msg("failed to find lists for plate")
		return nil, fmt.Errorf("failed to find lists for plate: %w", err)
	}

	if len(hits) > 0 {
		s.log.Info().
			Int64("plate_id", plateID).
			Str("plate", normalized).
			Int("hits_count", len(hits)).
			Msg("plate found in lists")
		for _, hit := range hits {
			s.log.Debug().
				Int64("list_id", hit.ListID).
				Str("list_name", hit.ListName).
				Str("list_type", hit.ListType).
				Msg("list hit")
		}
	} else {
		s.log.Debug().
			Int64("plate_id", plateID).
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
			ID:            p.ID,
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
		info := EventInfo{
			ID:              e.ID,
			PlateID:         e.PlateID,
			CameraID:        e.CameraID,
			CameraModel:     e.CameraModel,
			Direction:       e.Direction,
			Lane:            e.Lane,
			RawPlate:        e.RawPlate,
			NormalizedPlate: e.NormalizedPlate,
			Confidence:      e.Confidence,
			VehicleColor:    e.VehicleColor,
			VehicleType:     e.VehicleType,
			SnapshotURL:     e.SnapshotURL,
			EventTime:       e.EventTime,
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

type PlateInfo struct {
	ID            int64      `json:"id"`
	Number        string     `json:"number"`
	Normalized    string     `json:"normalized"`
	LastEventTime *time.Time `json:"last_event_time,omitempty"`
}

type EventInfo struct {
	ID              int64      `json:"id"`
	PlateID         *int64     `json:"plate_id,omitempty"`
	CameraID        string     `json:"camera_id"`
	CameraModel     *string    `json:"camera_model,omitempty"`
	Direction       *string    `json:"direction,omitempty"`
	Lane            *int       `json:"lane,omitempty"`
	RawPlate        string     `json:"raw_plate"`
	NormalizedPlate string     `json:"normalized_plate"`
	Confidence      *float64   `json:"confidence,omitempty"`
	VehicleColor    *string    `json:"vehicle_color,omitempty"`
	VehicleType     *string    `json:"vehicle_type,omitempty"`
	SnapshotURL     *string    `json:"snapshot_url,omitempty"`
	EventTime       time.Time  `json:"event_time"`
}

