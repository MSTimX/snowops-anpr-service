package db

import (
	"fmt"

	"gorm.io/gorm"
)

var migrationStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`,
	`CREATE TABLE IF NOT EXISTS plates (
		id              BIGSERIAL PRIMARY KEY,
		number          TEXT NOT NULL,
		normalized      TEXT NOT NULL,
		country         TEXT,
		region          TEXT,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_plates_normalized ON plates(normalized);`,
	`CREATE TABLE IF NOT EXISTS vehicles (
		id              BIGSERIAL PRIMARY KEY,
		plate_id        BIGINT REFERENCES plates(id),
		make            TEXT,
		model           TEXT,
		color           TEXT,
		body_type       TEXT,
		notes           TEXT,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE TABLE IF NOT EXISTS anpr_events (
		id              BIGSERIAL PRIMARY KEY,
		plate_id        BIGINT REFERENCES plates(id),
		camera_id       TEXT NOT NULL,
		camera_model    TEXT,
		direction       TEXT,
		lane            INT,
		raw_plate       TEXT NOT NULL,
		normalized_plate TEXT NOT NULL,
		confidence      NUMERIC(5,2),
		vehicle_color   TEXT,
		vehicle_type    TEXT,
		snapshot_url    TEXT,
		event_time      TIMESTAMPTZ NOT NULL,
		raw_payload     JSONB,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_plate_id ON anpr_events(plate_id);`,
	`CREATE INDEX IF NOT EXISTS idx_anpr_events_event_time ON anpr_events(event_time);`,
	`CREATE TABLE IF NOT EXISTS lists (
		id          BIGSERIAL PRIMARY KEY,
		name        TEXT NOT NULL,
		type        TEXT NOT NULL,
		description TEXT,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_lists_name ON lists(name);`,
	`CREATE TABLE IF NOT EXISTS list_items (
		list_id     BIGINT REFERENCES lists(id),
		plate_id    BIGINT REFERENCES plates(id),
		note        TEXT,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		PRIMARY KEY (list_id, plate_id)
	);`,
	`DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM lists WHERE name = 'default_whitelist') THEN
			INSERT INTO lists (name, type, description) VALUES ('default_whitelist', 'WHITELIST', 'Default whitelist');
		END IF;
		IF NOT EXISTS (SELECT 1 FROM lists WHERE name = 'default_blacklist') THEN
			INSERT INTO lists (name, type, description) VALUES ('default_blacklist', 'BLACKLIST', 'Default blacklist');
		END IF;
	END
	$$;`,
}

func runMigrations(db *gorm.DB) error {
	for i, stmt := range migrationStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}
	return nil
}

