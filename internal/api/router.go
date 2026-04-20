package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/vprdemo/fleet-dispatch/internal/dispatch"
	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/order"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

type Router struct {
	mux        *http.ServeMux
	vehicleSvc *vehicle.Service
	orderSvc   *order.Service
	dispatcher *dispatch.Engine
}

func NewRouter(vs *vehicle.Service, os *order.Service, d *dispatch.Engine) *Router {
	r := &Router{
		mux:        http.NewServeMux(),
		vehicleSvc: vs,
		orderSvc:   os,
		dispatcher: d,
	}
	r.routes()
	return r
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if req.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	r.mux.ServeHTTP(w, req)
}

func (r *Router) routes() {
	// Vehicle endpoints
	r.mux.HandleFunc("GET /api/vehicles", r.listVehicles)
	r.mux.HandleFunc("GET /api/vehicles/{id}", r.getVehicle)
	r.mux.HandleFunc("POST /api/vehicles", r.registerVehicle)

	// Order endpoints
	r.mux.HandleFunc("GET /api/orders", r.listOrders)
	r.mux.HandleFunc("GET /api/orders/{id}", r.getOrder)
	r.mux.HandleFunc("POST /api/orders", r.createOrder)
	r.mux.HandleFunc("POST /api/orders/{id}/dispatch", r.dispatchOrder)
	r.mux.HandleFunc("PUT /api/orders/{id}/status", r.updateOrderStatus)

	// Health
	r.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Static files (web dashboard)
	webDir := "web"
	if _, err := os.Stat(webDir); err == nil {
		r.mux.Handle("GET /", http.FileServer(http.Dir(webDir)))
	}
}

// --- Vehicle handlers ---

func (r *Router) listVehicles(w http.ResponseWriter, req *http.Request) {
	vehicles, err := r.vehicleSvc.ListVehicles(req.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, vehicles)
}

func (r *Router) getVehicle(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	v, err := r.vehicleSvc.GetVehicle(req.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (r *Router) registerVehicle(w http.ResponseWriter, req *http.Request) {
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.ID == "" || body.Name == "" {
		writeErr(w, http.StatusBadRequest, nil)
		return
	}
	v, err := r.vehicleSvc.Register(req.Context(), body.ID, body.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// --- Order handlers ---

func (r *Router) listOrders(w http.ResponseWriter, req *http.Request) {
	status := req.URL.Query().Get("status")
	orders, err := r.orderSvc.List(req.Context(), status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

func (r *Router) getOrder(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	o, err := r.orderSvc.Get(req.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (r *Router) createOrder(w http.ResponseWriter, req *http.Request) {
	var o model.Order
	if err := json.NewDecoder(req.Body).Decode(&o); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	created, err := r.orderSvc.Create(req.Context(), &o)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (r *Router) dispatchOrder(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if err := r.dispatcher.DispatchOrder(req.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "dispatched"})
}

func (r *Router) updateOrderStatus(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body struct {
		Status model.OrderStatus `json:"status"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	o, err := r.orderSvc.Transition(req.Context(), id, body.Status)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	msg := "error"
	if err != nil {
		msg = err.Error()
	}
	writeJSON(w, code, map[string]string{"error": msg})
}
