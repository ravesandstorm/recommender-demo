package redisstore

import (
	"context"
	"fmt"
	"time"
)

func seenKey(userID string) string {
	return fmt.Sprintf("user:%s:seen", userID)
}

// EnsureSeen loads the user's seen set into Redis if missing.
// hydrate returns post ID strings to seed the set (may be empty).
func (s *Store) EnsureSeen(ctx context.Context, userID string, ttl time.Duration, hydrate func(context.Context) ([]string, error)) error {
	key := seenKey(userID)
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ids, err := hydrate(ctx)
	if err != nil {
		return err
	}
	// Even with zero IDs, create an empty set marker via a placeholder we immediately
	// remove — better: SADD nothing can't create key. Use SETNX-style empty set by
	// adding then removing a sentinel, or just EXPIRE after optional SADD.
	pipe := s.client.Pipeline()
	// Sentinel keeps the key alive when the user has zero views (empty SETs are deleted).
	pipe.SAdd(ctx, key, "__init__")
	if len(ids) > 0 {
		members := make([]any, len(ids))
		for i, id := range ids {
			members[i] = id
		}
		pipe.SAdd(ctx, key, members...)
	}
	pipe.Expire(ctx, key, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

// FilterUnseen returns ids that are not in the user's seen set, preserving order.
func (s *Store) FilterUnseen(ctx context.Context, userID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	key := seenKey(userID)
	members := make([]any, len(ids))
	for i, id := range ids {
		members[i] = id
	}
	flags, err := s.client.SMIsMember(ctx, key, members...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for i, id := range ids {
		if i < len(flags) && flags[i] {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// MarkSeen adds post IDs to the seen set and refreshes TTL.
func (s *Store) MarkSeen(ctx context.Context, userID string, ids []string, ttl time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	key := seenKey(userID)
	members := make([]any, len(ids))
	for i, id := range ids {
		members[i] = id
	}
	pipe := s.client.Pipeline()
	pipe.SAdd(ctx, key, members...)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteSeenKey removes the seen set (tests / forced re-hydrate).
func (s *Store) DeleteSeenKey(ctx context.Context, userID string) error {
	return s.client.Del(ctx, seenKey(userID)).Err()
}
