package dispatch

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"

	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/mqtt"
	"github.com/vprdemo/fleet-dispatch/internal/order"
	"github.com/vprdemo/fleet-dispatch/internal/store"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

// Engine is the core dispatch engine using a greedy nearest-vehicle algorithm.
type Engine struct {
	vehicleSvc *vehicle.Service
	orderSvc   *order.Service
	redis      *store.RedisStore
	mqttH      *mqtt.Handler
	mu         sync.Mutex
}

func NewEngine(vs *vehicle.Service, os *order.Service, redis *store.RedisStore, mqttH *mqtt.Handler) *Engine {
	return &Engine{
		vehicleSvc: vs,
		orderSvc:   os,
		redis:      redis,
		mqttH:      mqttH,
	}
}

// DispatchOrder finds the nearest idle vehicle and assigns it to the given order.
func (e *Engine) DispatchOrder(ctx context.Context, orderID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	o, err := e.orderSvc.Get(ctx, orderID)
	if err != nil {
		return fmt.Errorf("get order: %w", err)
	}
	if o.Status != model.OrderStatusPending {
		return fmt.Errorf("order %s is not pending (current: %s)", orderID, o.Status)
	}

	// Get all vehicles from Redis cache for fast matching
	vehicles, err := e.redis.GetAllVehicles(ctx)
	if err != nil || len(vehicles) == 0 {
		// Fallback to DB
		vehicles, err = e.vehicleSvc.ListVehicles(ctx)
		if err != nil {
			return fmt.Errorf("list vehicles: %w", err)
		}
	}

	best := findNearestIdle(vehicles, o.PickupLat, o.PickupLng)
	if best == nil {
		return fmt.Errorf("no idle vehicle available")
	}

	// Assign order
	if err := e.orderSvc.Assign(ctx, orderID, best.ID); err != nil {
		return fmt.Errorf("assign order: %w", err)
	}

	// Update vehicle status
	if err := e.vehicleSvc.SetStatus(ctx, best.ID, model.VehicleStatusEnRoute); err != nil {
		log.Printf("dispatch: failed to update vehicle status: %v", err)
	}

	// Send command to vehicle via MQTT
	if e.mqttH != nil {
		cmd := map[string]interface{}{
			"action":      "pickup",
			"order_id":    orderID,
			"pickup_lat":  o.PickupLat,
			"pickup_lng":  o.PickupLng,
			"dropoff_lat": o.DropoffLat,
			"dropoff_lng": o.DropoffLng,
		}
		if err := e.mqttH.PublishCommand(best.ID, cmd); err != nil {
			log.Printf("dispatch: failed to publish command to %s: %v", best.ID, err)
		}
	}

	log.Printf("dispatch: assigned order %s to vehicle %s (dist=%.4f km)",
		orderID, best.ID, haversine(o.PickupLat, o.PickupLng, best.Latitude, best.Longitude))
	return nil
}

func findNearestIdle(vehicles []*model.Vehicle, lat, lng float64) *model.Vehicle {
	var best *model.Vehicle
	bestDist := math.MaxFloat64

	for _, v := range vehicles {
		if v.Status != model.VehicleStatusIdle {
			continue
		}
		if v.Battery < 20 { // skip low-battery vehicles
			continue
		}
		d := haversine(lat, lng, v.Latitude, v.Longitude)
		if d < bestDist {
			bestDist = d
			best = v
		}
	}
	return best
}

// haversine returns distance in km between two coordinates.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
