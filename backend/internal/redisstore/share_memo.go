package redisstore

import (
	"context"
	"fmt"
	"time"
)

func shareMemoKey(userID, postID string) string {
	return fmt.Sprintf("user:%s:shared:%s", userID, postID)
}

// HasShareMemo reports whether the user already shared this post (Redis memo).
func (s *Store) HasShareMemo(ctx context.Context, userID, postID string) (bool, error) {
	n, err := s.client.Exists(ctx, shareMemoKey(userID, postID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetShareMemo records a share with TTL. Uses SET NX so concurrent writers are safe.
func (s *Store) SetShareMemo(ctx context.Context, userID, postID string, ttl time.Duration) error {
	return s.client.Set(ctx, shareMemoKey(userID, postID), "1", ttl).Err()
}
