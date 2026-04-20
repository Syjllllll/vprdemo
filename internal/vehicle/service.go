package vehicle

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/store"
)

type Service struct {
	db    *sql.DB
	redis *store.RedisStore
}

func NewService(db *sql.DB, redis *store.RedisStore) *Service {
	return &Service{db: db, redis: redis}
}

func (s *Service) Register(ctx context.Context, id, name string) (*model.Vehicle, error) {
	now := time.Now()
	v := &model.Vehicle{
		ID:        id,
		Name:      name,
		Status:    model.VehicleStatusOffline,
		UpdatedAt: now,
		CreatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO vehicles (id, name, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO UPDATE SET name=$2, updated_at=$5`,
		v.ID, v.Name, v.Status, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("register vehicle: %w", err)
	}
	return v, nil
}

func (s *Service) UpdateTelemetry(ctx context.Context, t *model.VehicleTelemetry) error {
	now := time.Now()

	// Update Redis cache (hot path)
	v, err := s.redis.GetVehicle(ctx, t.VehicleID)
	if err != nil {
		v = &model.Vehicle{ID: t.VehicleID}
	}
	v.Latitude = t.Latitude
	v.Longitude = t.Longitude
	v.Battery = t.Battery
	v.Speed = t.Speed
	v.UpdatedAt = now
	if v.Status == model.VehicleStatusOffline {
		v.Status = model.VehicleStatusIdle
	}

	if err := s.redis.SetVehicle(ctx, v); err != nil {
		log.Printf("redis set vehicle %s: %v", t.VehicleID, err)
	}

	// Persist to DB (can be batched/async later)
	_, err = s.db.ExecContext(ctx,
		`UPDATE vehicles SET latitude=$1, longitude=$2, battery=$3, speed=$4, updated_at=$5,
		 status = CASE WHEN status='offline' THEN 'idle' ELSE status END
		 WHERE id=$6`,
		t.Latitude, t.Longitude, t.Battery, t.Speed, now, t.VehicleID,
	)
	if err != nil {
		return fmt.Errorf("update telemetry: %w", err)
	}
	return nil
}

func (s *Service) GetVehicle(ctx context.Context, id string) (*model.Vehicle, error) {
	// Try Redis first
	if v, err := s.redis.GetVehicle(ctx, id); err == nil {
		return v, nil
	}
	// Fallback to DB
	v := &model.Vehicle{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, status, latitude, longitude, battery, speed, order_id, updated_at, created_at
		 FROM vehicles WHERE id=$1`, id,
	).Scan(&v.ID, &v.Name, &v.Status, &v.Latitude, &v.Longitude,
		&v.Battery, &v.Speed, &v.OrderID, &v.UpdatedAt, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get vehicle: %w", err)
	}
	_ = s.redis.SetVehicle(ctx, v)
	return v, nil
}

func (s *Service) ListVehicles(ctx context.Context) ([]*model.Vehicle, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, status, latitude, longitude, battery, speed, order_id, updated_at, created_at
		 FROM vehicles ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vehicles []*model.Vehicle
	for rows.Next() {
		v := &model.Vehicle{}
		if err := rows.Scan(&v.ID, &v.Name, &v.Status, &v.Latitude, &v.Longitude,
			&v.Battery, &v.Speed, &v.OrderID, &v.UpdatedAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		vehicles = append(vehicles, v)
	}
	return vehicles, rows.Err()
}

func (s *Service) SetStatus(ctx context.Context, id string, status model.VehicleStatus) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE vehicles SET status=$1, updated_at=$2 WHERE id=$3`,
		status, now, id,
	)
	if err != nil {
		return err
	}
	if v, err := s.redis.GetVehicle(ctx, id); err == nil {
		v.Status = status
		v.UpdatedAt = now
		_ = s.redis.SetVehicle(ctx, v)
	}
	return nil
}
