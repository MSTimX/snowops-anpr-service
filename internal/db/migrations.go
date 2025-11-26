package db

import (
	"fmt"

	"gorm.io/gorm"
)

var migrationStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`,

	// Таблица plates - хранит все уникальные номера с нормализацией
	// Связь с vehicles через normalized (логическая связь через vehicles.plate_number)
	`CREATE TABLE IF NOT EXISTS anpr_plates (
		id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		number          TEXT NOT NULL,
		normalized      TEXT NOT NULL,
		country         TEXT,
		region          TEXT,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_anpr_plates_normalized ON anpr_plates(normalized);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_plates_number ON anpr_plates(number);`,

	// Таблица anpr_events - события распознавания номеров
	// camera_id может быть UUID (если камера из основной БД) или TEXT (внешний ID камеры)
	`CREATE TABLE IF NOT EXISTS anpr_events (
		id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		plate_id        UUID REFERENCES anpr_plates(id) ON DELETE SET NULL,
		camera_id       TEXT NOT NULL,
		camera_uuid     UUID,
		polygon_id      UUID,
		camera_model    TEXT,
		direction       TEXT,
		lane            INT,
		raw_plate       TEXT NOT NULL,
		normalized_plate TEXT NOT NULL,
		confidence      NUMERIC(5,2),
		vehicle_color   TEXT,
		vehicle_type    TEXT,
		vehicle_brand   TEXT,
		vehicle_model   TEXT,
		vehicle_country TEXT,
		vehicle_plate_color TEXT,
		vehicle_speed   NUMERIC(7,2),
		snapshot_url    TEXT,
		event_time      TIMESTAMPTZ NOT NULL,
		raw_payload     JSONB,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_plate_id ON anpr_events(plate_id);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_event_time ON anpr_events(event_time);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_normalized_plate ON anpr_events(normalized_plate);`,
	// Добавляем столбец camera_uuid, если его нет (для существующих таблиц)
	`DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
			WHERE table_name = 'anpr_events' AND column_name = 'camera_uuid') THEN
			ALTER TABLE anpr_events ADD COLUMN camera_uuid UUID;
		END IF;
	END
	$$;`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_camera_uuid ON anpr_events(camera_uuid) WHERE camera_uuid IS NOT NULL;`,
	// Добавляем столбец polygon_id, если его нет (для существующих таблиц)
	`DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns 
			WHERE table_name = 'anpr_events' AND column_name = 'polygon_id') THEN
			ALTER TABLE anpr_events ADD COLUMN polygon_id UUID;
		END IF;
	END
	$$;`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_polygon_id ON anpr_events(polygon_id) WHERE polygon_id IS NOT NULL;`,
	`ALTER TABLE anpr_events ADD COLUMN IF NOT EXISTS vehicle_brand TEXT;`,
	`ALTER TABLE anpr_events ADD COLUMN IF NOT EXISTS vehicle_model TEXT;`,
	`ALTER TABLE anpr_events ADD COLUMN IF NOT EXISTS vehicle_country TEXT;`,
	`ALTER TABLE anpr_events ADD COLUMN IF NOT EXISTS vehicle_plate_color TEXT;`,
	`ALTER TABLE anpr_events ADD COLUMN IF NOT EXISTS vehicle_speed NUMERIC(7,2);`,

	// Таблица lists - списки номеров (whitelist/blacklist)
	`CREATE TABLE IF NOT EXISTS anpr_lists (
		id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		name        TEXT NOT NULL,
		type        TEXT NOT NULL,
		description TEXT,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_anpr_lists_name ON anpr_lists(name);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_lists_type ON anpr_lists(type);`,

	// Таблица list_items - связи номеров со списками
	`CREATE TABLE IF NOT EXISTS anpr_list_items (
		list_id     UUID REFERENCES anpr_lists(id) ON DELETE CASCADE,
		plate_id    UUID REFERENCES anpr_plates(id) ON DELETE CASCADE,
		note        TEXT,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (list_id, plate_id)
	);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_list_items_plate_id ON anpr_list_items(plate_id);`,

	// Создание дефолтных списков
	`DO $$
	DECLARE
		whitelist_id UUID;
		blacklist_id UUID;
	BEGIN
		-- Создаем default_whitelist если его нет
		IF NOT EXISTS (SELECT 1 FROM anpr_lists WHERE name = 'default_whitelist') THEN
			INSERT INTO anpr_lists (id, name, type, description) 
			VALUES (uuid_generate_v4(), 'default_whitelist', 'WHITELIST', 'Default whitelist - автоматически добавляются номера из vehicles')
			RETURNING id INTO whitelist_id;
		END IF;
		
		-- Создаем default_blacklist если его нет
		IF NOT EXISTS (SELECT 1 FROM anpr_lists WHERE name = 'default_blacklist') THEN
			INSERT INTO anpr_lists (id, name, type, description) 
			VALUES (uuid_generate_v4(), 'default_blacklist', 'BLACKLIST', 'Default blacklist')
			RETURNING id INTO blacklist_id;
		END IF;
	END
	$$;`,

	// Функция для нормализации номера (аналогична Go функции)
	// Используется в триггерах для автоматической синхронизации
	`CREATE OR REPLACE FUNCTION normalize_plate_number(plate_text TEXT)
	RETURNS TEXT AS $$
	BEGIN
		-- Удаляем все пробелы, дефисы и приводим к верхнему регистру
		RETURN UPPER(REGEXP_REPLACE(plate_text, '[^A-Z0-9]', '', 'g'));
	END;
	$$ LANGUAGE plpgsql IMMUTABLE;`,

	// Функция для автоматического добавления номера в whitelist при создании vehicle
	// Вызывается извне (через API или триггер в основной БД, если нужно)
	`CREATE OR REPLACE FUNCTION anpr_sync_vehicle_to_whitelist(vehicle_plate_number TEXT)
	RETURNS UUID AS $$
	DECLARE
		normalized_plate TEXT;
		plate_uuid UUID;
		whitelist_uuid UUID;
	BEGIN
		-- Нормализуем номер
		normalized_plate := normalize_plate_number(vehicle_plate_number);
		
		IF normalized_plate = '' THEN
			RETURN NULL;
		END IF;
		
		-- Получаем или создаем plate
		SELECT id INTO plate_uuid
		FROM anpr_plates
		WHERE normalized = normalized_plate;
		
		IF plate_uuid IS NULL THEN
			INSERT INTO anpr_plates (number, normalized)
			VALUES (vehicle_plate_number, normalized_plate)
			RETURNING id INTO plate_uuid;
		END IF;
		
		-- Получаем ID whitelist
		SELECT id INTO whitelist_uuid
		FROM anpr_lists
		WHERE name = 'default_whitelist' AND type = 'WHITELIST'
		LIMIT 1;
		
		IF whitelist_uuid IS NULL THEN
			-- Создаем whitelist если его нет
			INSERT INTO anpr_lists (name, type, description)
			VALUES ('default_whitelist', 'WHITELIST', 'Default whitelist')
			RETURNING id INTO whitelist_uuid;
		END IF;
		
		-- Добавляем номер в whitelist (если еще не добавлен)
		INSERT INTO anpr_list_items (list_id, plate_id, note)
		VALUES (whitelist_uuid, plate_uuid, 'Автоматически добавлен из vehicles')
		ON CONFLICT (list_id, plate_id) DO NOTHING;
		
		RETURN plate_uuid;
	END;
	$$ LANGUAGE plpgsql;`,

	// Индекс для быстрого поиска по normalized_plate в anpr_events
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_normalized_plate_time ON anpr_events(normalized_plate, event_time DESC);`,
}

func runMigrations(db *gorm.DB) error {
	for i, stmt := range migrationStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}
	return nil
}
