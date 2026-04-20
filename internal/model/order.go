package model

import "time"

type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusAssigned  OrderStatus = "assigned"
	OrderStatusPickedUp  OrderStatus = "picked_up"
	OrderStatusDelivered OrderStatus = "delivered"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusFailed    OrderStatus = "failed"
)

type Order struct {
	ID             string      `json:"id" db:"id"`
	Status         OrderStatus `json:"status" db:"status"`
	VehicleID      string      `json:"vehicle_id,omitempty" db:"vehicle_id"`
	PickupLat      float64     `json:"pickup_lat" db:"pickup_lat"`
	PickupLng      float64     `json:"pickup_lng" db:"pickup_lng"`
	DropoffLat     float64     `json:"dropoff_lat" db:"dropoff_lat"`
	DropoffLng     float64     `json:"dropoff_lng" db:"dropoff_lng"`
	PickupAddr     string      `json:"pickup_addr" db:"pickup_addr"`
	DropoffAddr    string      `json:"dropoff_addr" db:"dropoff_addr"`
	AssignedAt     *time.Time  `json:"assigned_at,omitempty" db:"assigned_at"`
	PickedUpAt     *time.Time  `json:"picked_up_at,omitempty" db:"picked_up_at"`
	DeliveredAt    *time.Time  `json:"delivered_at,omitempty" db:"delivered_at"`
	CreatedAt      time.Time   `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at" db:"updated_at"`
}

// Valid state transitions for order status machine.
var validTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusPending:   {OrderStatusAssigned, OrderStatusCancelled},
	OrderStatusAssigned:  {OrderStatusPickedUp, OrderStatusCancelled, OrderStatusFailed},
	OrderStatusPickedUp:  {OrderStatusDelivered, OrderStatusFailed},
	OrderStatusDelivered: {},
	OrderStatusCancelled: {},
	OrderStatusFailed:    {OrderStatusPending}, // allow retry
}

func (o *Order) CanTransitionTo(next OrderStatus) bool {
	allowed, ok := validTransitions[o.Status]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == next {
			return true
		}
	}
	return false
}
