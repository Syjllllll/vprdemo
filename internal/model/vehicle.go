package model

import "time"

type VehicleStatus string

const (
	VehicleStatusIdle       VehicleStatus = "idle"
	VehicleStatusEnRoute    VehicleStatus = "en_route"
	VehicleStatusDelivering VehicleStatus = "delivering"
	VehicleStatusCharging   VehicleStatus = "charging"
	VehicleStatusOffline    VehicleStatus = "offline"
	VehicleStatusFault      VehicleStatus = "fault"
)

type Vehicle struct {
	ID        string        `json:"id" db:"id"`
	Name      string        `json:"name" db:"name"`
	Status    VehicleStatus `json:"status" db:"status"`
	Latitude  float64       `json:"latitude" db:"latitude"`
	Longitude float64       `json:"longitude" db:"longitude"`
	Battery   float64       `json:"battery" db:"battery"` // 0-100
	Speed     float64       `json:"speed" db:"speed"`     // km/h
	OrderID   string        `json:"order_id,omitempty" db:"order_id"`
	UpdatedAt time.Time     `json:"updated_at" db:"updated_at"`
	CreatedAt time.Time     `json:"created_at" db:"created_at"`
}

// VehicleTelemetry is the real-time data reported by the vehicle via MQTT.
type VehicleTelemetry struct {
	VehicleID string  `json:"vehicle_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Battery   float64 `json:"battery"`
	Speed     float64 `json:"speed"`
	Timestamp int64   `json:"timestamp"`
}
