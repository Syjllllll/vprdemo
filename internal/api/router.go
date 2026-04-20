package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/vprdemo/fleet-dispatch/internal/dispatch"
	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/order"
	"github.com/vprdemo/fleet-dispatch/internal/simulation"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

type Router struct {
	mux        *http.ServeMux
	vehicleSvc *vehicle.Service
	orderSvc   *order.Service
	dispatcher *dispatch.Engine
	simMgr     *simulation.Manager
}

func NewRouter(vs *vehicle.Service, os *order.Service, d *dispatch.Engine, sim *simulation.Manager) *Router {
	r := &Router{
		mux:        http.NewServeMux(),
		vehicleSvc: vs,
		orderSvc:   os,
		dispatcher: d,
		simMgr:     sim,
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

	// Simulation endpoints
	r.mux.HandleFunc("POST /api/sim/start", r.simStart)
	r.mux.HandleFunc("POST /api/sim/stop", r.simStop)
	r.mux.HandleFunc("GET /api/sim/status", r.simStatus)
	r.mux.HandleFunc("POST /api/sim/order", r.simCreateOrder)
	r.mux.HandleFunc("POST /api/sim/batch", r.simBatch)

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

// --- Simulation handlers ---

func (r *Router) simStart(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Count int `json:"count"`
	}
	body.Count = 5 // 默认值
	json.NewDecoder(req.Body).Decode(&body)
	if body.Count < 1 {
		body.Count = 1
	}
	if body.Count > 20 {
		body.Count = 20
	}
	if err := r.simMgr.Start(req.Context(), body.Count); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "started", "count": body.Count})
}

func (r *Router) simStop(w http.ResponseWriter, req *http.Request) {
	r.simMgr.Stop()
	writeJSON(w, http.StatusOK, map[string]string{"message": "stopped"})
}

func (r *Router) simStatus(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, r.simMgr.GetStatus())
}

func (r *Router) simCreateOrder(w http.ResponseWriter, req *http.Request) {
	var body struct {
		AutoDispatch bool `json:"auto_dispatch"`
	}
	body.AutoDispatch = true
	json.NewDecoder(req.Body).Decode(&body)

	o, err := r.simMgr.CreateRandomOrder(req.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	if body.AutoDispatch {
		// 调度
		if err := r.dispatcher.DispatchOrder(req.Context(), o.ID); err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"order": o, "dispatch": "failed: " + err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"order": o, "dispatch": "ok"})
}

func (r *Router) simBatch(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Count        int  `json:"count"`
		AutoDispatch bool `json:"auto_dispatch"`
	}
	body.Count = 3
	body.AutoDispatch = true
	json.NewDecoder(req.Body).Decode(&body)
	if body.Count < 1 {
		body.Count = 1
	}
	if body.Count > 20 {
		body.Count = 20
	}

	results := make([]map[string]interface{}, 0, body.Count)
	for i := 0; i < body.Count; i++ {
		o, err := r.simMgr.CreateRandomOrder(req.Context())
		if err != nil {
			results = append(results, map[string]interface{}{"error": err.Error()})
			continue
		}
		dispatchResult := "skipped"
		if body.AutoDispatch {
			if err := r.dispatcher.DispatchOrder(req.Context(), o.ID); err != nil {
				dispatchResult = "failed: " + err.Error()
			} else {
				dispatchResult = "ok"
			}
		}
		results = append(results, map[string]interface{}{
			"order_id":    o.ID,
			"pickup":      o.PickupAddr,
			"dropoff":     o.DropoffAddr,
			"dispatch":    dispatchResult,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"results": results, "total": len(results)})
}
