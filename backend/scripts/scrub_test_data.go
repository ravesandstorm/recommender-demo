//go:build ignore

package main

// One-shot scrub of leftover integration-test users/posts from local demo DBs.
// Usage from backend/: go run ./scripts/scrub_test_data.go

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	defer store.Pool.Close()

	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "redis: %v\n", err)
		os.Exit(1)
	}
	q := qdrantclient.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.VectorDim)

	rows, err := store.Pool.Query(ctx, `
		SELECT id, username FROM users
		WHERE username ~ '^(tester_|del_|ixlist_|like_user_|feed_user_|retain_|seen_|vw_|ix_)'
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list users: %v\n", err)
		os.Exit(1)
	}
	type urow struct {
		id   uuid.UUID
		name string
	}
	var users []urow
	for rows.Next() {
		var u urow
		if err := rows.Scan(&u.id, &u.name); err != nil {
			rows.Close()
			fmt.Fprintf(os.Stderr, "scan user: %v\n", err)
			os.Exit(1)
		}
		users = append(users, u)
	}
	rows.Close()

	for _, u := range users {
		n, _ := rdb.DeleteUserKeys(ctx, u.id.String())
		if err := store.DeleteUser(ctx, u.id); err != nil {
			fmt.Fprintf(os.Stderr, "delete user %s: %v\n", u.name, err)
			continue
		}
		fmt.Printf("deleted user %s (%s) redis_keys=%d\n", u.name, u.id, n)
	}

	postRows, err := store.Pool.Query(ctx, `
		SELECT id FROM posts
		WHERE title IN ('R','VW','IX','Seen','Del post','List post','A','B','Soccer tactics pressing')
		   OR content LIKE 'retain %'
		   OR content LIKE 'viewwriter %'
		   OR content LIKE 'interactionwriter %'
		   OR content LIKE 'hydrate %'
		   OR content LIKE 'delete user test %'
		   OR content LIKE 'interaction list %'
		   OR content LIKE 'content a %'
		   OR content LIKE 'content b %'
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list posts: %v\n", err)
		os.Exit(1)
	}
	var postIDs []uuid.UUID
	var postIDStrs []string
	for postRows.Next() {
		var id uuid.UUID
		if err := postRows.Scan(&id); err != nil {
			postRows.Close()
			fmt.Fprintf(os.Stderr, "scan post: %v\n", err)
			os.Exit(1)
		}
		postIDs = append(postIDs, id)
		postIDStrs = append(postIDStrs, id.String())
	}
	postRows.Close()

	if len(postIDs) > 0 {
		_ = q.DeletePoints(ctx, postIDStrs)
		if err := store.DeletePosts(ctx, postIDs); err != nil {
			fmt.Fprintf(os.Stderr, "delete posts: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("deleted %d test posts (+ qdrant points)\n", len(postIDs))
	} else {
		fmt.Println("no leftover test posts")
	}

	_ = rdb.DeleteRecentKey(ctx)
	fmt.Println("scrub done")
}
