package redisstore

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	client *redis.Client
	dim    int
}

func New(addr string, dim int) *Store {
	return &Store{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		dim:    dim,
	}
}

func (s *Store) Client() *redis.Client {
	return s.client
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func userKey(userID string) string {
	return fmt.Sprintf("user:%s:vector", userID)
}

func (s *Store) GetUserVector(ctx context.Context, userID string) ([]float32, error) {
	data, err := s.client.Get(ctx, userKey(userID)).Bytes()
	if err == redis.Nil {
		return make([]float32, s.dim), nil
	}
	if err != nil {
		return nil, err
	}
	return bytesToFloat32s(data), nil
}

func (s *Store) SetUserVector(ctx context.Context, userID string, vec []float32) error {
	return s.client.Set(ctx, userKey(userID), float32sToBytes(vec), 0).Err()
}

func float32sToBytes(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToFloat32s(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}
