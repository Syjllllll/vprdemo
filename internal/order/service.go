package order

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/vprdemo/fleet-dispatch/internal/model"
)

type Service struct {
	db *sql.DB
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

func (s *Service) Create(ctx context.Context, o *model.Order) (*model.Order, error) {
	now := time.Now()
	o.ID = uuid.NewString()
	o.Status = model.OrderStatusPending
	o.CreatedAt = now
	o.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO orders (id, status, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng, pickup_addr, dropoff_addr, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		o.ID, o.Status, o.PickupLat, o.PickupLng, o.DropoffLat, o.DropoffLng,
		o.PickupAddr, o.DropoffAddr, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create order: %w", err)
	}
	return o, nil
}

func (s *Service) Get(ctx context.Context, id string) (*model.Order, error) {
	o := &model.Order{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, status, vehicle_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
		 pickup_addr, dropoff_addr, assigned_at, picked_up_at, delivered_at, created_at, updated_at
		 FROM orders WHERE id=$1`, id,
	).Scan(&o.ID, &o.Status, &o.VehicleID, &o.PickupLat, &o.PickupLng,
		&o.DropoffLat, &o.DropoffLng, &o.PickupAddr, &o.DropoffAddr,
		&o.AssignedAt, &o.PickedUpAt, &o.DeliveredAt, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	return o, nil
}

func (s *Service) List(ctx context.Context, status string) ([]*model.Order, error) {
	query := `SELECT id, status, vehicle_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
		 pickup_addr, dropoff_addr, assigned_at, picked_up_at, delivered_at, created_at, updated_at
		 FROM orders`
	var args []interface{}
	if status != "" {
		query += " WHERE status=$1"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []*model.Order
	for rows.Next() {
		o := &model.Order{}
		if err := rows.Scan(&o.ID, &o.Status, &o.VehicleID, &o.PickupLat, &o.PickupLng,
			&o.DropoffLat, &o.DropoffLng, &o.PickupAddr, &o.DropoffAddr,
			&o.AssignedAt, &o.PickedUpAt, &o.DeliveredAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func (s *Service) Transition(ctx context.Context, id string, newStatus model.OrderStatus) (*model.Order, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !o.CanTransitionTo(newStatus) {
		return nil, fmt.Errorf("invalid transition from %s to %s", o.Status, newStatus)
	}
	now := time.Now()
	o.Status = newStatus
	o.UpdatedAt = now

	switch newStatus {
	case model.OrderStatusAssigned:
		o.AssignedAt = &now
	case model.OrderStatusPickedUp:
		o.PickedUpAt = &now
	case model.OrderStatusDelivered:
		o.DeliveredAt = &now
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE orders SET status=$1, assigned_at=$2, picked_up_at=$3, delivered_at=$4, updated_at=$5
		 WHERE id=$6`,
		o.Status, o.AssignedAt, o.PickedUpAt, o.DeliveredAt, o.UpdatedAt, o.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("transition order: %w", err)
	}
	return o, nil
}

func (s *Service) Assign(ctx context.Context, orderID, vehicleID string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE orders SET vehicle_id=$1, status='assigned', assigned_at=$2, updated_at=$2 WHERE id=$3`,
		vehicleID, now, orderID,
	)
	return err
}
