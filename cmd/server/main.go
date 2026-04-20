package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/vprdemo/fleet-dispatch/internal/api"
	"github.com/vprdemo/fleet-dispatch/internal/config"
	"github.com/vprdemo/fleet-dispatch/internal/dispatch"
	"github.com/vprdemo/fleet-dispatch/internal/mqtt"
	"github.com/vprdemo/fleet-dispatch/internal/order"
	"github.com/vprdemo/fleet-dispatch/internal/simulation"
	"github.com/vprdemo/fleet-dispatch/internal/store"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

func main() {
	cfg := config.Load()

	// Database
	db, err := store.NewPostgres(cfg.DBDSN())
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	if err := store.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("database connected and migrated")

	// Redis
	redis, err := store.NewRedis(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("redis connected")

	// Services
	vehicleSvc := vehicle.NewService(db, redis)
	orderSvc := order.NewService(db)

	// MQTT (optional — don't crash if broker is unavailable)
	var mqttHandler *mqtt.Handler
	mqttHandler, err = mqtt.NewHandler(cfg.MQTTBroker, vehicleSvc)
	if err != nil {
		log.Printf("mqtt: broker unavailable, running without MQTT: %v", err)
		mqttHandler = nil
	} else {
		defer mqttHandler.Close()
		log.Println("mqtt connected")
	}

	// Dispatch engine
	dispatcher := dispatch.NewEngine(vehicleSvc, orderSvc, redis, mqttHandler)

	// Simulator manager (in-process)
	simMgr := simulation.NewManager(vehicleSvc, orderSvc, cfg.MQTTBroker)

	// HTTP server
	router := api.NewRouter(vehicleSvc, orderSvc, dispatcher, simMgr)
	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	server := &http.Server{Addr: addr, Handler: router}

	go func() {
		log.Printf("http server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	simMgr.Stop()
	server.Close()
}
