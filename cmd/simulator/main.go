package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// 模拟器配置
var (
	apiBase    = flag.String("api", "http://localhost:8080", "API 服务地址")
	mqttBroker = flag.String("mqtt", "tcp://localhost:1884", "MQTT broker 地址")
	count      = flag.Int("count", 5, "模拟车辆数量")
	interval   = flag.Duration("interval", 3*time.Second, "遥测上报间隔")
	centerLat  = flag.Float64("lat", 39.9042, "模拟区域中心纬度")
	centerLng  = flag.Float64("lng", 116.4074, "模拟区域中心经度")
	radius     = flag.Float64("radius", 0.02, "模拟区域半径(经纬度)")
)

// 命令载荷
type Command struct {
	Action     string  `json:"action"`
	OrderID    string  `json:"order_id"`
	PickupLat  float64 `json:"pickup_lat"`
	PickupLng  float64 `json:"pickup_lng"`
	DropoffLat float64 `json:"dropoff_lat"`
	DropoffLng float64 `json:"dropoff_lng"`
}

// 遥测载荷
type Telemetry struct {
	VehicleID string  `json:"vehicle_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Battery   float64 `json:"battery"`
	Speed     float64 `json:"speed"`
	Timestamp int64   `json:"timestamp"`
}

// 状态载荷
type StatusMsg struct {
	VehicleID string `json:"vehicle_id"`
	Status    string `json:"status"`
}

// 虚拟车辆
type VirtualVehicle struct {
	ID      string
	Name    string
	Lat     float64
	Lng     float64
	Battery float64
	Speed   float64
	Status  string // idle | en_route | delivering | charging

	// 当前任务
	mu         sync.Mutex
	targetLat  float64
	targetLng  float64
	dropoffLat float64
	dropoffLng float64
	orderID    string
	phase      string // "" | "to_pickup" | "to_dropoff"
	client     mqtt.Client
	apiBase    string
	stopCh     chan struct{}
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	fmt.Println("========================================")
	fmt.Println("  自动配送车辆车队模拟器")
	fmt.Println("========================================")
	fmt.Printf("API 地址:   %s\n", *apiBase)
	fmt.Printf("MQTT 地址:  %s\n", *mqttBroker)
	fmt.Printf("车辆数量:   %d\n", *count)
	fmt.Printf("上报间隔:   %s\n", *interval)
	fmt.Printf("模拟中心:   %.4f, %.4f\n", *centerLat, *centerLng)
	fmt.Println("========================================")

	vehicles := make([]*VirtualVehicle, *count)
	for i := 0; i < *count; i++ {
		vid := fmt.Sprintf("sim-%03d", i+1)
		vname := fmt.Sprintf("模拟车辆-%03d", i+1)
		// 在中心区域随机散布
		lat := *centerLat + (rand.Float64()-0.5)*2*(*radius)
		lng := *centerLng + (rand.Float64()-0.5)*2*(*radius)

		vehicles[i] = &VirtualVehicle{
			ID:      vid,
			Name:    vname,
			Lat:     lat,
			Lng:     lng,
			Battery: 60 + rand.Float64()*40, // 60-100%
			Speed:   0,
			Status:  "idle",
			apiBase: *apiBase,
			stopCh:  make(chan struct{}),
		}
	}

	// 1. 通过 API 注册所有车辆
	fmt.Println("\n[1/3] 注册车辆...")
	for _, v := range vehicles {
		if err := v.Register(); err != nil {
			log.Printf("  ⚠ 注册 %s 失败: %v (可能已存在，继续)", v.ID, err)
		} else {
			log.Printf("  ✓ 注册 %s (%s)", v.ID, v.Name)
		}
	}

	// 2. 连接 MQTT
	fmt.Println("\n[2/3] 连接 MQTT...")
	for _, v := range vehicles {
		if err := v.ConnectMQTT(*mqttBroker); err != nil {
			log.Fatalf("  ✗ %s MQTT 连接失败: %v", v.ID, err)
		}
		log.Printf("  ✓ %s MQTT 已连接", v.ID)
	}

	// 3. 启动模拟循环
	fmt.Println("\n[3/3] 启动模拟...")
	for _, v := range vehicles {
		go v.RunLoop(*interval)
	}

	fmt.Println("\n✅ 模拟器已启动！所有车辆正在上报遥测数据。")
	fmt.Println("   车辆会自动响应调度命令（接单→取货→送达）")
	fmt.Println("   按 Ctrl+C 停止模拟器")
	fmt.Println()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n正在停止模拟器...")
	for _, v := range vehicles {
		close(v.stopCh)
		v.PublishStatus("offline")
		if v.client != nil && v.client.IsConnected() {
			v.client.Disconnect(500)
		}
	}
	fmt.Println("模拟器已停止。")
}

// Register 通过 HTTP API 注册车辆
func (v *VirtualVehicle) Register() error {
	body, _ := json.Marshal(map[string]string{
		"id":   v.ID,
		"name": v.Name,
	})
	resp, err := http.Post(v.apiBase+"/api/vehicles", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 && resp.StatusCode != 409 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// ConnectMQTT 连接到 MQTT broker 并订阅命令 topic
func (v *VirtualVehicle) ConnectMQTT(broker string) error {
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID("sim-" + v.ID).
		SetAutoReconnect(true).
		SetKeepAlive(30 * time.Second).
		SetCleanSession(true)

	v.client = mqtt.NewClient(opts)
	token := v.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	// 订阅调度命令
	cmdTopic := fmt.Sprintf("fleet/command/%s", v.ID)
	v.client.Subscribe(cmdTopic, 1, v.onCommand)

	// 上报初始状态
	v.PublishStatus("idle")
	return nil
}

// onCommand 处理服务器下发的调度命令
func (v *VirtualVehicle) onCommand(client mqtt.Client, msg mqtt.Message) {
	var cmd Command
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		log.Printf("[%s] 解析命令失败: %v", v.ID, err)
		return
	}

	log.Printf("[%s] 📦 收到命令: action=%s order=%s", v.ID, cmd.Action, cmd.OrderID)

	v.mu.Lock()
	defer v.mu.Unlock()

	switch cmd.Action {
	case "pickup":
		v.orderID = cmd.OrderID
		v.targetLat = cmd.PickupLat
		v.targetLng = cmd.PickupLng
		v.dropoffLat = cmd.DropoffLat
		v.dropoffLng = cmd.DropoffLng
		v.phase = "to_pickup"
		v.Status = "en_route"
		log.Printf("[%s] 🚗 前往取货点 (%.4f, %.4f)", v.ID, cmd.PickupLat, cmd.PickupLng)
	case "cancel":
		v.orderID = ""
		v.phase = ""
		v.Status = "idle"
		log.Printf("[%s] ❌ 任务已取消", v.ID)
	}
}

// RunLoop 是车辆的主模拟循环
func (v *VirtualVehicle) RunLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-v.stopCh:
			return
		case <-ticker.C:
			v.tick()
			v.publishTelemetry()
		}
	}
}

// tick 更新车辆状态（移动、电量消耗等）
func (v *VirtualVehicle) tick() {
	v.mu.Lock()
	defer v.mu.Unlock()

	switch v.phase {
	case "to_pickup", "to_dropoff":
		// 向目标点移动
		arrived := v.moveToward(v.targetLat, v.targetLng)
		if arrived {
			if v.phase == "to_pickup" {
				// 到达取货点，切换到送货
				log.Printf("[%s] 📍 到达取货点，开始配送", v.ID)
				v.phase = "to_dropoff"
				v.targetLat = v.dropoffLat
				v.targetLng = v.dropoffLng
				v.Status = "delivering"
				v.PublishStatus("delivering")
				// 通知服务器已取货
				v.updateOrderStatus(v.orderID, "picked_up")
			} else {
				// 到达送达点，任务完成
				log.Printf("[%s] ✅ 配送完成！订单 %s", v.ID, v.orderID)
				v.updateOrderStatus(v.orderID, "delivered")
				v.orderID = ""
				v.phase = ""
				v.Status = "idle"
				v.Speed = 0
				v.PublishStatus("idle")
			}
		}

	default:
		// 空闲状态，随机巡游
		v.Speed = 5 + rand.Float64()*10 // 5-15 km/h 慢速巡游
		v.Lat += (rand.Float64() - 0.5) * 0.0005
		v.Lng += (rand.Float64() - 0.5) * 0.0005
	}

	// 电量消耗
	if v.Speed > 0 {
		v.Battery -= 0.02 * (v.Speed / 30)
	}
	// 低电量自动充电
	if v.Battery < 15 {
		v.Status = "charging"
		v.phase = ""
		v.Speed = 0
		v.PublishStatus("charging")
		log.Printf("[%s] 🔋 电量低 (%.1f%%)，开始充电", v.ID, v.Battery)
	}
	if v.Status == "charging" {
		v.Battery += 0.5
		if v.Battery >= 95 {
			v.Battery = 95
			v.Status = "idle"
			v.PublishStatus("idle")
			log.Printf("[%s] 🔋 充电完成", v.ID)
		}
	}
	if v.Battery < 0 {
		v.Battery = 0
	}
}

// moveToward 向目标点移动，返回是否到达
func (v *VirtualVehicle) moveToward(tLat, tLng float64) bool {
	dLat := tLat - v.Lat
	dLng := tLng - v.Lng
	dist := math.Sqrt(dLat*dLat + dLng*dLng)

	if dist < 0.0003 { // 约 30 米内视为到达
		v.Lat = tLat
		v.Lng = tLng
		v.Speed = 0
		return true
	}

	// 模拟 20-40 km/h 的速度
	v.Speed = 20 + rand.Float64()*20
	// 每个 tick 移动的经纬度步长（约每秒移动一小段）
	step := 0.001 + rand.Float64()*0.0005
	if step > dist {
		step = dist
	}
	v.Lat += dLat / dist * step
	v.Lng += dLng / dist * step
	return false
}

// publishTelemetry 上报遥测数据
func (v *VirtualVehicle) publishTelemetry() {
	t := Telemetry{
		VehicleID: v.ID,
		Latitude:  v.Lat,
		Longitude: v.Lng,
		Battery:   math.Round(v.Battery*10) / 10,
		Speed:     math.Round(v.Speed*10) / 10,
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(t)
	topic := fmt.Sprintf("fleet/telemetry/%s", v.ID)
	v.client.Publish(topic, 1, false, data)
}

// PublishStatus 上报状态
func (v *VirtualVehicle) PublishStatus(status string) {
	msg := StatusMsg{
		VehicleID: v.ID,
		Status:    status,
	}
	data, _ := json.Marshal(msg)
	topic := fmt.Sprintf("fleet/status/%s", v.ID)
	v.client.Publish(topic, 1, false, data)
}

// updateOrderStatus 通过 API 更新订单状态
func (v *VirtualVehicle) updateOrderStatus(orderID, status string) {
	body, _ := json.Marshal(map[string]string{"status": status})
	url := fmt.Sprintf("%s/api/orders/%s/status", v.apiBase, orderID)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[%s] 更新订单状态失败: %v", v.ID, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("[%s] 订单 %s 状态 → %s (HTTP %d)", v.ID, orderID, status, resp.StatusCode)
}