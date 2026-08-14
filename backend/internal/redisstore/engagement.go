package redisstore

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	engLikeVal    = "1"
	engDislikeVal = "0"
	engTombstone  = "x"
	engActiveVal  = "1"
)

func engLikesKey(userID string) string {
	return fmt.Sprintf("user:%s:eng:likes", userID)
}

func engSavesKey(userID string) string {
	return fmt.Sprintf("user:%s:eng:saves", userID)
}

func engSharesKey(userID string) string {
	return fmt.Sprintf("user:%s:eng:shares", userID)
}

// GetEngLikeField returns the raw hash value and whether the field exists.
// Values: "1" like, "0" dislike, "x" tombstone.
func (s *Store) GetEngLikeField(ctx context.Context, userID, postID string) (val string, found bool, err error) {
	val, err = s.client.HGet(ctx, engLikesKey(userID), postID).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// GetEngLike returns hot like/dislike state. exists=false if missing or tombstoned.
func (s *Store) GetEngLike(ctx context.Context, userID, postID string) (isLike bool, exists bool, err error) {
	val, found, err := s.GetEngLikeField(ctx, userID, postID)
	if err != nil || !found || val == engTombstone {
		return false, false, err
	}
	if val == engLikeVal {
		return true, true, nil
	}
	if val == engDislikeVal {
		return false, true, nil
	}
	return false, false, nil
}

// SetEngLike stores like (true) or dislike (false) in the hot hash.
func (s *Store) SetEngLike(ctx context.Context, userID, postID string, isLike bool) error {
	v := engDislikeVal
	if isLike {
		v = engLikeVal
	}
	return s.client.HSet(ctx, engLikesKey(userID), postID, v).Err()
}

// ClearEngLike tombstones a like/dislike so list merges hide the PG row until flush.
func (s *Store) ClearEngLike(ctx context.Context, userID, postID string) error {
	return s.client.HSet(ctx, engLikesKey(userID), postID, engTombstone).Err()
}

// AllEngLikes returns postID → "1"|"0"|"x" for merge reads.
func (s *Store) AllEngLikes(ctx context.Context, userID string) (map[string]string, error) {
	m, err := s.client.HGetAll(ctx, engLikesKey(userID)).Result()
	if err != nil {
		return nil, err
	}
	return m, nil
}

// HasEngSave reports whether the post is saved in the hot hash (active, not tombstone).
func (s *Store) HasEngSave(ctx context.Context, userID, postID string) (bool, error) {
	val, found, err := s.GetEngSaveField(ctx, userID, postID)
	if err != nil || !found {
		return false, err
	}
	return val == engActiveVal, nil
}

func (s *Store) GetEngSaveField(ctx context.Context, userID, postID string) (val string, found bool, err error) {
	val, err = s.client.HGet(ctx, engSavesKey(userID), postID).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (s *Store) SetEngSave(ctx context.Context, userID, postID string) error {
	return s.client.HSet(ctx, engSavesKey(userID), postID, engActiveVal).Err()
}

func (s *Store) ClearEngSave(ctx context.Context, userID, postID string) error {
	return s.client.HSet(ctx, engSavesKey(userID), postID, engTombstone).Err()
}

func (s *Store) AllEngSaves(ctx context.Context, userID string) (map[string]string, error) {
	return s.client.HGetAll(ctx, engSavesKey(userID)).Result()
}

// HasEngShare reports an active hot share.
func (s *Store) HasEngShare(ctx context.Context, userID, postID string) (bool, error) {
	val, err := s.client.HGet(ctx, engSharesKey(userID), postID).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val == engActiveVal, nil
}

func (s *Store) SetEngShare(ctx context.Context, userID, postID string) error {
	return s.client.HSet(ctx, engSharesKey(userID), postID, engActiveVal).Err()
}

func (s *Store) AllEngShares(ctx context.Context, userID string) (map[string]string, error) {
	return s.client.HGetAll(ctx, engSharesKey(userID)).Result()
}
