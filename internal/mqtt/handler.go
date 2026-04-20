package mqtt

import (
	"context"
	"encoding/json"
	"log"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

type Handler struct {
	client     pahomqtt.Client
	vehicleSvc *vehicle.Service
}

func NewHandler(brokerURL string, vehicleSvc *vehicle.Service) (*Handler, error) {
	opts := pahomqtt.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID("fleet-dispatch-server").
		SetAutoReconnect(true).
		SetCleanSession(true)

	client := pahomqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	h := &Handler{client: client, vehicleSvc: vehicleSvc}
	h.subscribe()
	return h, nil
}

func (h *Handler) subscribe() {
	// Vehicle telemetry: fleet/telemetry/{vehicle_id}
	h.client.Subscribe("fleet/telemetry/+", 1, func(_ pahomqtt.Client, msg pahomqtt.Message) {
		var t model.VehicleTelemetry
		if err := json.Unmarshal(msg.Payload(), &t); err != nil {
			log.Printf("mqtt: unmarshal telemetry: %v", err)
			return
		}
		if err := h.vehicleSvc.UpdateTelemetry(context.Background(), &t); err != nil {
			log.Printf("mqtt: update telemetry: %v", err)
		}
	})

	// Vehicle status: fleet/status/{vehicle_id}
	h.client.Subscribe("fleet/status/+", 1, func(_ pahomqtt.Client, msg pahomqtt.Message) {
		var payload struct {
			VehicleID string               `json:"vehicle_id"`
			Status    model.VehicleStatus  `json:"status"`
		}
		if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
			log.Printf("mqtt: unmarshal status: %v", err)
			return
		}
		if err := h.vehicleSvc.SetStatus(context.Background(), payload.VehicleID, payload.Status); err != nil {
			log.Printf("mqtt: set status: %v", err)
		}
	})

	log.Println("mqtt: subscribed to fleet/telemetry/+ and fleet/status/+")
}

// PublishCommand sends a command to a specific vehicle.
func (h *Handler) PublishCommand(vehicleID string, command interface{}) error {
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	topic := "fleet/command/" + vehicleID
	token := h.client.Publish(topic, 1, false, data)
	token.Wait()
	return token.Error()
}

func (h *Handler) Close() {
	h.client.Disconnect(1000)
}
