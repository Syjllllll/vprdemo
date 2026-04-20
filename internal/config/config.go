package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort   int
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	RedisAddr  string
	MQTTBroker string
}

func Load() *Config {
	return &Config{
		HTTPPort:   envInt("HTTP_PORT", 8080),
		DBHost:     envStr("DB_HOST", "localhost"),
		DBPort:     envInt("DB_PORT", 5432),
		DBUser:     envStr("DB_USER", "postgres"),
		DBPassword: envStr("DB_PASSWORD", "postgres"),
		DBName:     envStr("DB_NAME", "fleet_dispatch"),
		RedisAddr:  envStr("REDIS_ADDR", "localhost:6380"),
		MQTTBroker: envStr("MQTT_BROKER", "tcp://localhost:1884"),
	}
}

func (c *Config) DBDSN() string {
	return "host=" + c.DBHost +
		" port=" + strconv.Itoa(c.DBPort) +
		" user=" + c.DBUser +
		" password=" + c.DBPassword +
		" dbname=" + c.DBName +
		" sslmode=disable"
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
