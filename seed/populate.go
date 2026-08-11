package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postSeed struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func main() {
	ctx := context.Background()
	databaseURL := envOr("DATABASE_URL", "postgres://recsys:recsys@localhost:5432/recsys?sslmode=disable")
	embeddingURL := envOr("EMBEDDING_URL", "http://localhost:8082")
	qdrantURL := envOr("QDRANT_URL", "http://localhost:6334")
	collection := envOr("QDRANT_COLLECTION", "posts")
	jsonPath := envOr("POSTS_JSON", filepath.Join(".", "posts.json"))

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		// try relative to this file's folder when run from repo root
		alt := filepath.Join("seed", "posts.json")
		raw, err = os.ReadFile(alt)
		if err != nil {
			log.Fatalf("read posts json: %v", err)
		}
		jsonPath = alt
	}
	log.Printf("loading posts from %s", jsonPath)

	var posts []postSeed
	if err := json.Unmarshal(raw, &posts); err != nil {
		log.Fatalf("parse json: %v", err)
	}
	if len(posts) == 0 {
		log.Fatal("no posts in json")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	if err := ensureCollection(ctx, qdrantURL, collection, 384); err != nil {
		log.Fatalf("qdrant collection: %v", err)
	}

	httpClient := &http.Client{Timeout: 180 * time.Second}

	for i, p := range posts {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `
			INSERT INTO posts (title, content) VALUES ($1, $2)
			RETURNING id
		`, p.Title, p.Content).Scan(&id)
		if err != nil {
			log.Fatalf("insert post %d: %v", i, err)
		}

		text := p.Title + "\n" + p.Content
		emb, err := embed(ctx, httpClient, embeddingURL, []string{text})
		if err != nil {
			log.Fatalf("embed post %d: %v", i, err)
		}
		if len(emb) != 1 || len(emb[0]) != 384 {
			log.Fatalf("unexpected embedding shape for post %d", i)
		}

		if err := upsertQdrant(ctx, httpClient, qdrantURL, collection, id.String(), emb[0]); err != nil {
			log.Fatalf("qdrant upsert post %d: %v", i, err)
		}
		log.Printf("[%d/%d] seeded %s — %s", i+1, len(posts), id, truncate(p.Title, 60))
	}
	log.Printf("done: %d posts", len(posts))
}

func embed(ctx context.Context, client *http.Client, base string, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"texts": texts})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

func ensureCollection(ctx context.Context, base, collection string, dim int) error {
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("%s/collections/%s", base, collection)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{"size": dim, "distance": "Cosine"},
	})
	put, _ := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	put.Header.Set("Content-Type", "application/json")
	putResp, err := client.Do(put)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 300 {
		return fmt.Errorf("create collection status %d", putResp.StatusCode)
	}
	return nil
}

func upsertQdrant(ctx context.Context, client *http.Client, base, collection, id string, vec []float32) error {
	payload, _ := json.Marshal(map[string]any{
		"points": []map[string]any{{
			"id":      id,
			"vector":  vec,
			"payload": map[string]any{"post_id": id},
		}},
	})
	url := fmt.Sprintf("%s/collections/%s/points?wait=true", base, collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upsert status %d", resp.StatusCode)
	}
	return nil
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
