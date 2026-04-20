package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vprdemo/fleet-dispatch/internal/model"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedis(addr string) (*RedisStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &RedisStore{client: client}, nil
}

func (r *RedisStore) Client() *redis.Client {
	return r.client
}

func vehicleKey(id string) string {
	return "vehicle:" + id
}

func (r *RedisStore) SetVehicle(ctx context.Context, v *model.Vehicle) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, vehicleKey(v.ID), data, 5*time.Minute).Err()
}

func (r *RedisStore) GetVehicle(ctx context.Context, id string) (*model.Vehicle, error) {
	data, err := r.client.Get(ctx, vehicleKey(id)).Bytes()
	if err != nil {
		return nil, err
	}
	var v model.Vehicle
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *RedisStore) GetAllVehicles(ctx context.Context) ([]*model.Vehicle, error) {
	keys, err := r.client.Keys(ctx, "vehicle:*").Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	vals, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	vehicles := make([]*model.Vehicle, 0, len(vals))
	for _, val := range vals {
		if val == nil {
			continue
		}
		var v model.Vehicle
		if err := json.Unmarshal([]byte(val.(string)), &v); err != nil {
			continue
		}
		vehicles = append(vehicles, &v)
	}
	return vehicles, nil
}
