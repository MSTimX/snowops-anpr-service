package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"anpr-service/internal/domain/anpr"
)

type ANPRRepository struct {
	db *gorm.DB
}

func NewANPRRepository(db *gorm.DB) *ANPRRepository {
	return &ANPRRepository{db: db}
}

type Plate struct {
	ID         int64     `gorm:"primaryKey"`
	Number     string    `gorm:"not null"`
	Normalized string    `gorm:"not null;uniqueIndex"`
	Country    *string
	Region     *string
	CreatedAt  time.Time
}

type ANPREvent struct {
	ID              int64                  `gorm:"primaryKey"`
	PlateID         *int64
	CameraID        string                 `gorm:"not null"`
	CameraModel     *string
	Direction       *string
	Lane            *int
	RawPlate        string                 `gorm:"not null"`
	NormalizedPlate string                 `gorm:"not null"`
	Confidence      *float64
	VehicleColor    *string
	VehicleType     *string
	SnapshotURL     *string
	EventTime       time.Time              `gorm:"not null"`
	RawPayload      map[string]interface{} `gorm:"type:jsonb"`
	CreatedAt       time.Time
}

type List struct {
	ID          int64     `gorm:"primaryKey"`
	Name        string    `gorm:"not null;uniqueIndex"`
	Type        string    `gorm:"not null"`
	Description *string
	CreatedAt   time.Time
}

type ListItem struct {
	ListID    int64     `gorm:"primaryKey"`
	PlateID   int64     `gorm:"primaryKey"`
	Note      *string
	CreatedAt time.Time
}

func (r *ANPRRepository) GetOrCreatePlate(ctx context.Context, normalized, original string) (int64, error) {
	var plate Plate
	err := r.db.WithContext(ctx).Where("normalized = ?", normalized).First(&plate).Error
	if err == nil {
		return plate.ID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}

	plate = Plate{
		Number:     original,
		Normalized: normalized,
		CreatedAt:  time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(&plate).Error; err != nil {
		return 0, err
	}
	return plate.ID, nil
}

func (r *ANPRRepository) CreateANPREvent(ctx context.Context, event *anpr.Event) error {
	dbEvent := ANPREvent{
		PlateID:         &event.PlateID,
		CameraID:        event.CameraID,
		RawPlate:        event.Plate,
		NormalizedPlate: event.NormalizedPlate,
		EventTime:       event.EventTime,
		CreatedAt:       time.Now(),
	}

	if event.CameraModel != "" {
		dbEvent.CameraModel = &event.CameraModel
	}
	if event.Direction != "" {
		dbEvent.Direction = &event.Direction
	}
	if event.Lane != 0 {
		dbEvent.Lane = &event.Lane
	}
	if event.Confidence != 0 {
		dbEvent.Confidence = &event.Confidence
	}
	if event.Vehicle.Color != "" {
		dbEvent.VehicleColor = &event.Vehicle.Color
	}
	if event.Vehicle.Type != "" {
		dbEvent.VehicleType = &event.Vehicle.Type
	}
	if event.SnapshotURL != "" {
		dbEvent.SnapshotURL = &event.SnapshotURL
	}
	if len(event.RawPayload) > 0 {
		dbEvent.RawPayload = event.RawPayload
	}

	if err := r.db.WithContext(ctx).Create(&dbEvent).Error; err != nil {
		return err
	}

	event.ID = dbEvent.ID
	return nil
}

func (r *ANPRRepository) FindListsForPlate(ctx context.Context, plateID int64) ([]anpr.ListHit, error) {
	var hits []anpr.ListHit

	err := r.db.WithContext(ctx).
		Table("list_items").
		Select("lists.id as list_id, lists.name as list_name, lists.type as list_type").
		Joins("JOIN lists ON list_items.list_id = lists.id").
		Where("list_items.plate_id = ?", plateID).
		Scan(&hits).Error

	if err != nil {
		return nil, err
	}

	return hits, nil
}

func (r *ANPRRepository) FindPlatesByNormalized(ctx context.Context, normalized string) ([]Plate, error) {
	var plates []Plate
	err := r.db.WithContext(ctx).
		Where("normalized = ?", normalized).
		Find(&plates).Error
	return plates, err
}

func (r *ANPRRepository) FindEvents(ctx context.Context, normalizedPlate *string, from, to *time.Time, limit, offset int) ([]ANPREvent, error) {
	query := r.db.WithContext(ctx).Model(&ANPREvent{})

	if normalizedPlate != nil {
		query = query.Where("normalized_plate = ?", *normalizedPlate)
	}
	if from != nil {
		query = query.Where("event_time >= ?", *from)
	}
	if to != nil {
		query = query.Where("event_time <= ?", *to)
	}

	query = query.Order("event_time DESC")

	if limit > 0 {
		query = query.Limit(limit)
		if limit > 100 {
			query = query.Limit(100)
		}
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	var events []ANPREvent
	err := query.Find(&events).Error
	return events, err
}

func (r *ANPRRepository) GetLastEventTimeForPlate(ctx context.Context, plateID int64) (*time.Time, error) {
	var event ANPREvent
	err := r.db.WithContext(ctx).
		Where("plate_id = ?", plateID).
		Order("event_time DESC").
		First(&event).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &event.EventTime, nil
}

