package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"anpr-service/internal/domain/anpr"
)

type ANPRRepository struct {
	db *gorm.DB
}

func NewANPRRepository(db *gorm.DB) *ANPRRepository {
	return &ANPRRepository{db: db}
}

func (Plate) TableName() string {
	return "anpr_plates"
}

func (ANPREvent) TableName() string {
	return "anpr_events"
}

func (List) TableName() string {
	return "anpr_lists"
}

func (ListItem) TableName() string {
	return "anpr_list_items"
}

type Plate struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Number     string    `gorm:"not null"`
	Normalized string    `gorm:"not null;uniqueIndex"`
	Country    *string
	Region     *string
	CreatedAt  time.Time
}

type ANPREvent struct {
	ID                uuid.UUID  `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	PlateID           *uuid.UUID `gorm:"type:uuid"`
	CameraID          string     `gorm:"not null"`
	CameraUUID        *uuid.UUID `gorm:"type:uuid"`
	PolygonID         *uuid.UUID `gorm:"type:uuid"`
	CameraModel       *string
	Direction         *string
	Lane              *int
	RawPlate          string `gorm:"not null"`
	NormalizedPlate   string `gorm:"not null"`
	Confidence        *float64
	VehicleColor      *string
	VehicleType       *string
	VehicleBrand      *string
	VehicleModel      *string
	VehicleCountry    *string
	VehiclePlateColor *string
	VehicleSpeed      *float64
	SnapshotURL       *string
	EventTime         time.Time      `gorm:"not null"`
	RawPayload        datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt         time.Time
}

type List struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name        string    `gorm:"not null;uniqueIndex"`
	Type        string    `gorm:"not null"`
	Description *string
	CreatedAt   time.Time
}

type ListItem struct {
	ListID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	PlateID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Note      *string
	CreatedAt time.Time
}

func (r *ANPRRepository) GetOrCreatePlate(ctx context.Context, normalized, original string) (uuid.UUID, error) {
	var plate Plate
	err := r.db.WithContext(ctx).Where("normalized = ?", normalized).First(&plate).Error
	if err == nil {
		return plate.ID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return uuid.Nil, err
	}

	plate = Plate{
		ID:         uuid.New(),
		Number:     original,
		Normalized: normalized,
		CreatedAt:  time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(&plate).Error; err != nil {
		return uuid.Nil, fmt.Errorf("failed to create plate: %w", err)
	}
	return plate.ID, nil
}

func (r *ANPRRepository) CreateANPREvent(ctx context.Context, event *anpr.Event) error {
	dbEvent := ANPREvent{
		ID:              uuid.New(),
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
	if event.Vehicle.Brand != "" {
		dbEvent.VehicleBrand = &event.Vehicle.Brand
	}
	if event.Vehicle.Model != "" {
		dbEvent.VehicleModel = &event.Vehicle.Model
	}
	if event.Vehicle.Country != "" {
		dbEvent.VehicleCountry = &event.Vehicle.Country
	}
	if event.Vehicle.PlateColor != "" {
		dbEvent.VehiclePlateColor = &event.Vehicle.PlateColor
	}
	if event.Vehicle.Speed != nil {
		dbEvent.VehicleSpeed = event.Vehicle.Speed
	}
	if event.SnapshotURL != "" {
		dbEvent.SnapshotURL = &event.SnapshotURL
	}
	if len(event.RawPayload) > 0 {
		raw, err := json.Marshal(event.RawPayload)
		if err != nil {
			return fmt.Errorf("marshal raw payload: %w", err)
		}
		dbEvent.RawPayload = datatypes.JSON(raw)
	}

	if err := r.db.WithContext(ctx).Create(&dbEvent).Error; err != nil {
		return fmt.Errorf("failed to create ANPR event in database: %w", err)
	}

	event.ID = dbEvent.ID
	return nil
}

func (r *ANPRRepository) FindListsForPlate(ctx context.Context, plateID uuid.UUID) ([]anpr.ListHit, error) {
	var hits []anpr.ListHit

	err := r.db.WithContext(ctx).
		Table("anpr_list_items").
		Select("anpr_lists.id as list_id, anpr_lists.name as list_name, anpr_lists.type as list_type").
		Joins("JOIN anpr_lists ON anpr_list_items.list_id = anpr_lists.id").
		Where("anpr_list_items.plate_id = ?", plateID).
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

func (r *ANPRRepository) GetLastEventTimeForPlate(ctx context.Context, plateID uuid.UUID) (*time.Time, error) {
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

// SyncVehicleToWhitelist синхронизирует номер из vehicles в whitelist
// Вызывается при создании/обновлении vehicle в roles сервисе
func (r *ANPRRepository) SyncVehicleToWhitelist(ctx context.Context, plateNumber string) (uuid.UUID, error) {
	var plateID uuid.UUID
	err := r.db.WithContext(ctx).
		Raw("SELECT anpr_sync_vehicle_to_whitelist(?)", plateNumber).
		Scan(&plateID).Error
	if err != nil {
		return uuid.Nil, fmt.Errorf("sync vehicle to whitelist: %w", err)
	}
	return plateID, nil
}

// DeleteOldEvents удаляет события старше указанного количества дней
func (r *ANPRRepository) DeleteOldEvents(ctx context.Context, days int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -days)
	result := r.db.WithContext(ctx).
		Where("created_at < ?", cutoffTime).
		Delete(&ANPREvent{})

	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}
