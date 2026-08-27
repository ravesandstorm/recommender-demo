package redisstore

import (
	"context"
	"fmt"
)

// DeleteUserKeys removes all per-user Redis keys (vector, interests, seen, engagement, share memos).
func (s *Store) DeleteUserKeys(ctx context.Context, userID string) (int, error) {
	if userID == "" {
		return 0, nil
	}
	keys := []string{
		userKey(userID),
		interestsKey(userID),
		seenKey(userID),
		engLikesKey(userID),
		engSavesKey(userID),
		engSharesKey(userID),
	}
	deleted, err := s.client.Del(ctx, keys...).Result()
	if err != nil {
		return int(deleted), err
	}
	n := int(deleted)

	// Share memos: user:{id}:shared:*
	pattern := fmt.Sprintf("user:%s:shared:*", userID)
	var cursor uint64
	for {
		batch, next, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return n, err
		}
		if len(batch) > 0 {
			d, err := s.client.Del(ctx, batch...).Result()
			if err != nil {
				return n, err
			}
			n += int(d)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return n, nil
}
