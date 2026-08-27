package redisstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/recsys/backend/internal/vector"
)

func interestsKey(userID string) string {
	return fmt.Sprintf("user:%s:interests", userID)
}

type interestsPayload struct {
	Interests []vector.Interest `json:"interests"`
}

func (s *Store) GetUserInterests(ctx context.Context, userID string) ([]vector.Interest, error) {
	data, err := s.client.Get(ctx, interestsKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p interestsPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return p.Interests, nil
}

func (s *Store) SetUserInterests(ctx context.Context, userID string, interests []vector.Interest) error {
	if interests == nil {
		interests = []vector.Interest{}
	}
	raw, err := json.Marshal(interestsPayload{Interests: interests})
	if err != nil {
		return err
	}
	return s.client.Set(ctx, interestsKey(userID), raw, 0).Err()
}

// FlushUserVectors deletes preference + seen keys.
func (s *Store) FlushUserVectors(ctx context.Context) (int, error) {
	var deleted int
	for _, pattern := range []string{"user:*:vector", "user:*:interests", "user:*:seen"} {
		var cursor uint64
		for {
			keys, next, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return deleted, err
			}
			if len(keys) > 0 {
				n, err := s.client.Del(ctx, keys...).Result()
				if err != nil {
					return deleted, err
				}
				deleted += int(n)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	return deleted, nil
}
