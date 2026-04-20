# Fleet Dispatch - 无人配送车队调度系统

## 快速启动

### 1. 启动基础设施 (PostgreSQL + Redis + EMQX)
```bash
docker compose up -d
```

### 2. 运行服务
```bash
go run ./cmd/server
```

服务默认监听 `:8080`

### 3. 环境变量
| 变量 | 默认值 | 说明 |
|------|--------|------|
| HTTP_PORT | 8080 | HTTP 端口 |
| DB_HOST | localhost | PostgreSQL 地址 |
| DB_PORT | 5432 | PostgreSQL 端口 |
| DB_USER | postgres | 数据库用户 |
| DB_PASSWORD | postgres | 数据库密码 |
| DB_NAME | fleet_dispatch | 数据库名 |
| REDIS_ADDR | localhost:6379 | Redis 地址 |
| MQTT_BROKER | tcp://localhost:1883 | MQTT Broker 地址 |

## API

### 车辆
- `GET    /api/vehicles`         — 车辆列表
- `GET    /api/vehicles/{id}`    — 车辆详情
- `POST   /api/vehicles`         — 注册车辆 `{ "id": "v001", "name": "配送车1号" }`

### 订单
- `GET    /api/orders`           — 订单列表 (可选 `?status=pending`)
- `GET    /api/orders/{id}`      — 订单详情
- `POST   /api/orders`           — 创建订单
- `POST   /api/orders/{id}/dispatch` — 触发调度
- `PUT    /api/orders/{id}/status`   — 更新状态 `{ "status": "picked_up" }`

### MQTT Topics
| Topic | 方向 | 说明 |
|-------|------|------|
| `fleet/telemetry/{vehicle_id}` | 车→云 | GPS/电量/速度遥测 |
| `fleet/status/{vehicle_id}` | 车→云 | 车辆状态变更 |
| `fleet/command/{vehicle_id}` | 云→车 | 调度指令下发 |

## 项目结构
```
cmd/server/          — 主入口
internal/
  api/               — HTTP 路由与处理器
  config/            — 配置加载
  dispatch/          — 调度引擎 (贪心算法)
  model/             — 数据模型
  mqtt/              — MQTT 接入处理
  order/             — 订单服务
  store/             — 数据存储 (PostgreSQL + Redis)
  vehicle/           — 车辆服务
web/                 — 前端管理后台 (TODO)
```
