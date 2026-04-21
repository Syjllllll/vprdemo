package simulation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/vprdemo/fleet-dispatch/internal/model"
	"github.com/vprdemo/fleet-dispatch/internal/order"
	"github.com/vprdemo/fleet-dispatch/internal/vehicle"
)

// 北京地标点，用于随机生成订单
var landmarks = []struct {
	Name string
	Lat  float64
	Lng  float64
}{
	{"北京站", 39.9028, 116.4274},
	{"天安门广场", 39.9055, 116.3976},
	{"王府井", 39.9145, 116.4105},
	{"西单商场", 39.9099, 116.3739},
	{"前门大街", 39.8993, 116.3988},
	{"东直门", 39.9417, 116.4318},
	{"国贸CBD", 39.9087, 116.4605},
	{"三里屯", 39.9334, 116.4530},
	{"中关村", 39.9818, 116.3118},
	{"望京SOHO", 39.9933, 116.4766},
	{"五道口", 39.9926, 116.3381},
	{"朝阳大悦城", 39.9218, 116.5159},
}

// LogEntry 日志条目
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"` // info | success | warn | error
	Message string `json:"message"`
}

// Status 模拟器状态
type Status struct {
	Running    bool       `json:"running"`
	VehicleIDs []string   `json:"vehicle_ids"`
	Logs       []LogEntry `json:"logs"`
}

// vehicleState 单辆模拟车辆的内部状态
type vehicleState struct {
	id         string
	name       string
	lat, lng   float64
	battery    float64
	speed      float64
	status     string
	phase      string
	targetLat  float64
	targetLng  float64
	dropoffLat float64
	dropoffLng float64
	orderID    string
	client     mqtt.Client
	mu         sync.Mutex
	cancelFn   context.CancelFunc
}

// Manager 内嵌模拟器，在服务进程内运行
type Manager struct {
	mu         sync.Mutex
	running    bool
	vehicles   map[string]*vehicleState
	vehicleSvc *vehicle.Service
	orderSvc   *order.Service
	mqttBroker string
	selfAddr   string // 自身 HTTP 地址，用于回调更新订单状态
	logs       []LogEntry
}

// NewManager 创建模拟器管理器
func NewManager(vs *vehicle.Service, os *order.Service, mqttBroker string) *Manager {
	return &Manager{
		vehicles:   make(map[string]*vehicleState),
		vehicleSvc: vs,
		orderSvc:   os,
		mqttBroker: mqttBroker,
		selfAddr:   "http://localhost:8080",
		logs:       make([]LogEntry, 0, 100),
	}
}

func (m *Manager) addLog(level, msg string) {
	entry := LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Level:   level,
		Message: msg,
	}
	m.logs = append(m.logs, entry)
	// 只保留最近 200 条
	if len(m.logs) > 200 {
		m.logs = m.logs[len(m.logs)-200:]
	}
	log.Printf("[SIM][%s] %s", level, msg)
}

// Start 启动 count 辆模拟车辆
func (m *Manager) Start(ctx context.Context, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("模拟器已在运行中")
	}

	m.addLog("info", fmt.Sprintf("启动模拟器，车辆数量: %d", count))

	for i := 1; i <= count; i++ {
		vid := fmt.Sprintf("sim-%03d", i)
		vname := fmt.Sprintf("模拟车辆-%03d", i)

		// 注册车辆（忽略已存在错误）
		_, err := m.vehicleSvc.Register(ctx, vid, vname)
		if err != nil {
			m.addLog("warn", fmt.Sprintf("注册 %s: %v (可能已存在)", vid, err))
		}

		// 随机初始位置（北京中心区域）
		lat := 39.9042 + (rand.Float64()-0.5)*0.04
		lng := 116.4074 + (rand.Float64()-0.5)*0.04

		battery := 60 + rand.Float64()*40

		// 直接写入遥测数据，同步将状态置为 idle（避免依赖 MQTT 异步更新）
		_ = m.vehicleSvc.UpdateTelemetry(ctx, &model.VehicleTelemetry{
			VehicleID: vid,
			Latitude:  lat,
			Longitude: lng,
			Battery:   battery,
			Speed:     0,
			Timestamp: time.Now().Unix(),
		})

		vs := &vehicleState{
			id:      vid,
			name:    vname,
			lat:     lat,
			lng:     lng,
			battery: battery,
			status:  "idle",
		}

		// 连接 MQTT
		if err := m.connectVehicleMQTT(vs); err != nil {
			m.addLog("warn", fmt.Sprintf("%s MQTT 连接失败: %v，将跳过 MQTT", vid, err))
		}

		// 启动模拟循环
		vCtx, cancel := context.WithCancel(ctx)
		vs.cancelFn = cancel
		m.vehicles[vid] = vs
		go m.runVehicle(vCtx, vs)
		m.addLog("success", fmt.Sprintf("车辆 %s (%s) 已启动", vid, vname))
	}

	m.running = true
	m.addLog("success", fmt.Sprintf("模拟器启动完成，%d 辆车辆在线", count))
	return nil
}

// Stop 停止所有模拟车辆
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.addLog("info", "正在停止模拟器...")
	for id, vs := range m.vehicles {
		vs.cancelFn()
		if vs.client != nil && vs.client.IsConnected() {
			m.publishStatus(vs, "offline")
			vs.client.Disconnect(500)
		}
		delete(m.vehicles, id)
	}
	m.running = false
	m.addLog("success", "模拟器已停止")
}

// GetStatus 获取当前状态
func (m *Manager) GetStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := make([]string, 0, len(m.vehicles))
	for id := range m.vehicles {
		ids = append(ids, id)
	}
	logs := make([]LogEntry, len(m.logs))
	copy(logs, m.logs)

	return Status{
		Running:    m.running,
		VehicleIDs: ids,
		Logs:       logs,
	}
}

// CreateRandomOrder 创建随机订单（调度由调用方负责）
func (m *Manager) CreateRandomOrder(ctx context.Context) (*model.Order, error) {
	pickup := landmarks[rand.Intn(len(landmarks))]
	dropoff := landmarks[rand.Intn(len(landmarks))]
	for dropoff.Name == pickup.Name {
		dropoff = landmarks[rand.Intn(len(landmarks))]
	}

	o := &model.Order{
		PickupLat:   pickup.Lat + (rand.Float64()-0.5)*0.002,
		PickupLng:   pickup.Lng + (rand.Float64()-0.5)*0.002,
		DropoffLat:  dropoff.Lat + (rand.Float64()-0.5)*0.002,
		DropoffLng:  dropoff.Lng + (rand.Float64()-0.5)*0.002,
		PickupAddr:  pickup.Name,
		DropoffAddr: dropoff.Name,
	}

	created, err := m.orderSvc.Create(ctx, o)
	if err != nil {
		m.addLog("error", fmt.Sprintf("创建订单失败: %v", err))
		return nil, err
	}

	m.addLog("info", fmt.Sprintf("创建订单: %s → %s", pickup.Name, dropoff.Name))
	return created, nil
}

// --- MQTT 相关 ---

func (m *Manager) connectVehicleMQTT(vs *vehicleState) error {
	opts := mqtt.NewClientOptions().
		AddBroker(m.mqttBroker).
		SetClientID("sim-" + vs.id).
		SetAutoReconnect(true).
		SetKeepAlive(30 * time.Second).
		SetCleanSession(true).
		SetConnectionLostHandler(func(c mqtt.Client, err error) {
			log.Printf("[SIM] %s MQTT 断开: %v", vs.id, err)
		})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}
	if !client.IsConnected() {
		return fmt.Errorf("连接超时")
	}

	vs.client = client

	// 订阅调度命令
	topic := fmt.Sprintf("fleet/command/%s", vs.id)
	client.Subscribe(topic, 1, func(_ mqtt.Client, msg mqtt.Message) {
		m.onCommand(vs, msg)
	})

	// 上报初始状态
	m.publishStatus(vs, "idle")
	return nil
}

func (m *Manager) onCommand(vs *vehicleState, msg mqtt.Message) {
	var cmd struct {
		Action     string  `json:"action"`
		OrderID    string  `json:"order_id"`
		PickupLat  float64 `json:"pickup_lat"`
		PickupLng  float64 `json:"pickup_lng"`
		DropoffLat float64 `json:"dropoff_lat"`
		DropoffLng float64 `json:"dropoff_lng"`
	}
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		return
	}

	vs.mu.Lock()
	defer vs.mu.Unlock()

	vs.orderID = cmd.OrderID
	vs.targetLat = cmd.PickupLat
	vs.targetLng = cmd.PickupLng
	vs.dropoffLat = cmd.DropoffLat
	vs.dropoffLng = cmd.DropoffLng
	vs.phase = "to_pickup"
	vs.status = "en_route"

	m.mu.Lock()
	m.addLog("info", fmt.Sprintf("🚗 %s 接单 %s，前往 %.4f,%.4f", vs.id, cmd.OrderID[:8], cmd.PickupLat, cmd.PickupLng))
	m.mu.Unlock()
}

func (m *Manager) publishStatus(vs *vehicleState, status string) {
	if vs.client == nil || !vs.client.IsConnected() {
		return
	}
	data, _ := json.Marshal(map[string]string{"vehicle_id": vs.id, "status": status})
	vs.client.Publish(fmt.Sprintf("fleet/status/%s", vs.id), 1, false, data)
}

func (m *Manager) publishTelemetry(vs *vehicleState) {
	if vs.client == nil || !vs.client.IsConnected() {
		return
	}
	data, _ := json.Marshal(map[string]interface{}{
		"vehicle_id": vs.id,
		"latitude":   math.Round(vs.lat*10000) / 10000,
		"longitude":  math.Round(vs.lng*10000) / 10000,
		"battery":    math.Round(vs.battery*10) / 10,
		"speed":      math.Round(vs.speed*10) / 10,
		"timestamp":  time.Now().Unix(),
	})
	vs.client.Publish(fmt.Sprintf("fleet/telemetry/%s", vs.id), 1, false, data)
}

func (m *Manager) runVehicle(ctx context.Context, vs *vehicleState) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick(vs)
			m.publishTelemetry(vs)
		}
	}
}

func (m *Manager) tick(vs *vehicleState) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	switch vs.phase {
	case "to_pickup", "to_dropoff":
		arrived := moveToward(vs, vs.targetLat, vs.targetLng)
		if arrived {
			if vs.phase == "to_pickup" {
				vs.phase = "to_dropoff"
				vs.targetLat = vs.dropoffLat
				vs.targetLng = vs.dropoffLng
				vs.status = "delivering"
				m.publishStatus(vs, "delivering")
				go m.callUpdateOrderStatus(vs.orderID, "picked_up")
				m.mu.Lock()
				m.addLog("info", fmt.Sprintf("📍 %s 取货完成，配送中", vs.id))
				m.mu.Unlock()
			} else {
				go m.callUpdateOrderStatus(vs.orderID, "delivered")
				m.mu.Lock()
				m.addLog("success", fmt.Sprintf("✅ %s 完成配送，订单 %s", vs.id, vs.orderID[:8]))
				m.mu.Unlock()
				vs.orderID = ""
				vs.phase = ""
				vs.status = "idle"
				vs.speed = 0
				m.publishStatus(vs, "idle")
			}
		}
	default:
		// 空闲巡游
		vs.speed = 5 + rand.Float64()*10
		vs.lat += (rand.Float64() - 0.5) * 0.0005
		vs.lng += (rand.Float64() - 0.5) * 0.0005
	}

	// 电量消耗与充电
	if vs.speed > 0 {
		vs.battery -= 0.05 * (vs.speed / 30)
	}
	if vs.battery < 15 && vs.status != "charging" && vs.phase == "" {
		vs.status = "charging"
		vs.speed = 0
		m.publishStatus(vs, "charging")
		m.mu.Lock()
		m.addLog("warn", fmt.Sprintf("🔋 %s 电量低 (%.0f%%)，充电中", vs.id, vs.battery))
		m.mu.Unlock()
	}
	if vs.status == "charging" {
		vs.battery += 0.8
		if vs.battery >= 95 {
			vs.battery = 95
			vs.status = "idle"
			m.publishStatus(vs, "idle")
			m.mu.Lock()
			m.addLog("success", fmt.Sprintf("🔋 %s 充电完成", vs.id))
			m.mu.Unlock()
		}
	}
	if vs.battery < 0 {
		vs.battery = 0
	}
}

func moveToward(vs *vehicleState, tLat, tLng float64) bool {
	dLat := tLat - vs.lat
	dLng := tLng - vs.lng
	dist := math.Sqrt(dLat*dLat + dLng*dLng)
	if dist < 0.0003 {
		vs.lat = tLat
		vs.lng = tLng
		vs.speed = 0
		return true
	}
	vs.speed = 20 + rand.Float64()*20
	step := 0.0015 + rand.Float64()*0.0005
	if step > dist {
		step = dist
	}
	vs.lat += dLat / dist * step
	vs.lng += dLng / dist * step
	return false
}

func (m *Manager) callUpdateOrderStatus(orderID, status string) {
	body, _ := json.Marshal(map[string]string{"status": status})
	url := fmt.Sprintf("%s/api/orders/%s/status", m.selfAddr, orderID)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[SIM] 更新订单状态失败: %v", err)
		return
	}
	defer resp.Body.Close()
}
