package redisstore

import (
	"context"
	"time"
)

const recentKey = "posts:recent"

// GetRecentIDs returns cached recent post ID strings (newest first).
func (s *Store) GetRecentIDs(ctx context.Context) ([]string, error) {
	ids, err := s.client.LRange(ctx, recentKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// SetRecentIDs replaces the recent list and refreshes TTL (newest first).
func (s *Store) SetRecentIDs(ctx context.Context, ids []string, size int, ttl time.Duration) error {
	if size < 1 {
		size = 1
	}
	if len(ids) > size {
		ids = ids[:size]
	}
	pipe := s.client.Pipeline()
	pipe.Del(ctx, recentKey)
	if len(ids) > 0 {
		members := make([]any, len(ids))
		for i, id := range ids {
			members[i] = id
		}
		pipe.RPush(ctx, recentKey, members...)
		// Store newest-first: we RPUSH in newest-first order from caller,
		// so LRANGE 0 -1 returns newest first.
		pipe.Expire(ctx, recentKey, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// PrependRecentID inserts a new post at the front of the recent list.
func (s *Store) PrependRecentID(ctx context.Context, postID string, size int, ttl time.Duration) error {
	if size < 1 {
		size = 1
	}
	pipe := s.client.Pipeline()
	pipe.LPush(ctx, recentKey, postID)
	pipe.LTrim(ctx, recentKey, 0, int64(size-1))
	pipe.Expire(ctx, recentKey, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteRecentKey clears the recent cache (tests).
func (s *Store) DeleteRecentKey(ctx context.Context) error {
	return s.client.Del(ctx, recentKey).Err()
}
