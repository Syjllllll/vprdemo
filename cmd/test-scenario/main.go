package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

// 测试场景脚本：自动创建订单并触发调度

var (
	apiBase    = flag.String("api", "http://localhost:8080", "API 服务地址")
	orderCount = flag.Int("orders", 3, "创建订单数量")
	autoDispatch = flag.Bool("dispatch", true, "是否自动触发调度")
	delay      = flag.Duration("delay", 2*time.Second, "每个订单之间的间隔")
	centerLat  = flag.Float64("lat", 39.9042, "区域中心纬度")
	centerLng  = flag.Float64("lng", 116.4074, "区域中心经度")
)

// 预定义的地址
var addresses = []struct {
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

type OrderResp struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	PickupAddr string `json:"pickup_addr"`
	DropoffAddr string `json:"dropoff_addr"`
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags)

	fmt.Println("========================================")
	fmt.Println("  订单批量测试工具")
	fmt.Println("========================================")

	// 先检查系统健康
	fmt.Println("\n[检查] 系统健康状态...")
	resp, err := http.Get(*apiBase + "/api/health")
	if err != nil {
		log.Fatalf("❌ 无法连接服务器: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("✅ 服务器正常")
	} else {
		log.Fatalf("❌ 服务器异常: HTTP %d", resp.StatusCode)
	}

	// 查看当前车辆
	fmt.Println("\n[查看] 当前在线车辆...")
	listVehicles()

	// 创建订单
	fmt.Printf("\n[创建] 将创建 %d 个测试订单...\n", *orderCount)
	orderIDs := make([]string, 0)

	for i := 0; i < *orderCount; i++ {
		pickup := addresses[rand.Intn(len(addresses))]
		dropoff := addresses[rand.Intn(len(addresses))]
		for dropoff.Name == pickup.Name {
			dropoff = addresses[rand.Intn(len(addresses))]
		}

		order := map[string]interface{}{
			"pickup_lat":   pickup.Lat + (rand.Float64()-0.5)*0.002,
			"pickup_lng":   pickup.Lng + (rand.Float64()-0.5)*0.002,
			"dropoff_lat":  dropoff.Lat + (rand.Float64()-0.5)*0.002,
			"dropoff_lng":  dropoff.Lng + (rand.Float64()-0.5)*0.002,
			"pickup_addr":  pickup.Name,
			"dropoff_addr": dropoff.Name,
		}

		body, _ := json.Marshal(order)
		resp, err := http.Post(*apiBase+"/api/orders", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("❌ 创建订单 #%d 失败: %v", i+1, err)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var orderResp OrderResp
		json.Unmarshal(respBody, &orderResp)

		if resp.StatusCode == 201 || resp.StatusCode == 200 {
			fmt.Printf("  ✅ 订单 #%d: %s → %s (ID: %s)\n", i+1, pickup.Name, dropoff.Name, orderResp.ID)
			orderIDs = append(orderIDs, orderResp.ID)
		} else {
			fmt.Printf("  ❌ 订单 #%d 创建失败: HTTP %d\n", i+1, resp.StatusCode)
		}

		time.Sleep(*delay)
	}

	// 自动调度
	if *autoDispatch && len(orderIDs) > 0 {
		fmt.Printf("\n[调度] 触发 %d 个订单的自动调度...\n", len(orderIDs))
		for i, oid := range orderIDs {
			resp, err := http.Post(fmt.Sprintf("%s/api/orders/%s/dispatch", *apiBase, oid), "application/json", nil)
			if err != nil {
				log.Printf("❌ 调度订单 %s 失败: %v", oid, err)
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 {
				fmt.Printf("  ✅ 订单 #%d (%s) 已调度\n", i+1, oid)
			} else {
				fmt.Printf("  ⚠ 订单 #%d (%s) 调度响应: HTTP %d - %s\n", i+1, oid, resp.StatusCode, string(respBody))
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// 等待并查看结果
	fmt.Println("\n[监控] 等待 10 秒后查看订单状态...")
	time.Sleep(10 * time.Second)

	fmt.Println("\n[结果] 订单最终状态:")
	for _, oid := range orderIDs {
		resp, err := http.Get(fmt.Sprintf("%s/api/orders/%s", *apiBase, oid))
		if err != nil {
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var order map[string]interface{}
		json.Unmarshal(respBody, &order)
		fmt.Printf("  📦 %s: 状态=%v 车辆=%v\n", oid, order["status"], order["vehicle_id"])
	}

	// 查看车辆状态
	fmt.Println("\n[结果] 车辆最终状态:")
	listVehicles()

	fmt.Println("\n✅ 测试完成！")
}

func listVehicles() {
	resp, err := http.Get(*apiBase + "/api/vehicles")
	if err != nil {
		log.Printf("获取车辆列表失败: %v", err)
		return
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var vehicles []map[string]interface{}
	json.Unmarshal(body, &vehicles)

	if len(vehicles) == 0 {
		fmt.Println("  (无车辆)")
		return
	}

	for _, v := range vehicles {
		fmt.Printf("  🚗 %s (%s) 状态=%s 电量=%.0f%% 位置=(%.4f, %.4f)\n",
			v["id"], v["name"], v["status"],
			toFloat(v["battery"]), toFloat(v["latitude"]), toFloat(v["longitude"]))
	}
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}
