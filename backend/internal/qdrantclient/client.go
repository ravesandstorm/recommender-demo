package qdrantclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type Client struct {
	baseURL    string
	collection string
	dim        int
	httpClient *http.Client
}

func New(baseURL, collection string, dim int) *Client {
	return &Client{
		baseURL:    baseURL,
		collection: collection,
		dim:        dim,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) EnsureCollection(ctx context.Context) error {
	url := fmt.Sprintf("%s/collections/%s", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	body := map[string]any{
		"vectors": map[string]any{
			"size":     c.dim,
			"distance": "Cosine",
		},
	}
	raw, _ := json.Marshal(body)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return err
	}
	defer putResp.Body.Close()
	if putResp.StatusCode >= 300 {
		return fmt.Errorf("create collection status %d", putResp.StatusCode)
	}
	return nil
}

type UpsertPoint struct {
	ID     string
	Vector []float32
}

func (c *Client) Upsert(ctx context.Context, points []UpsertPoint) error {
	payload := map[string]any{
		"points": make([]map[string]any, 0, len(points)),
	}
	pts := payload["points"].([]map[string]any)
	for _, p := range points {
		pts = append(pts, map[string]any{
			"id":      p.ID,
			"vector":  p.Vector,
			"payload": map[string]any{"post_id": p.ID},
		})
	}
	payload["points"] = pts

	raw, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/collections/%s/points?wait=true", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upsert status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetVector(ctx context.Context, postID string) ([]float32, error) {
	url := fmt.Sprintf("%s/collections/%s/points/%s", c.baseURL, c.collection, postID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get point status %d", resp.StatusCode)
	}
	var out struct {
		Result struct {
			Vector []float32 `json:"vector"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Result.Vector, nil
}

type SearchHit struct {
	ID     string
	Score  float64
	Vector []float32
}

func (c *Client) Search(ctx context.Context, vector []float32, limit int) ([]SearchHit, error) {
	body := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		"with_vector":  true,
	}
	raw, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/collections/%s/points/search", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search status %d", resp.StatusCode)
	}

	var out struct {
		Result []struct {
			ID      any       `json:"id"`
			Score   float64   `json:"score"`
			Vector  []float32 `json:"vector"`
			Payload struct {
				PostID string `json:"post_id"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(out.Result))
	for _, r := range out.Result {
		id := r.Payload.PostID
		if id == "" {
			id = stringifyID(r.ID)
		}
		hits = append(hits, SearchHit{ID: id, Score: r.Score, Vector: r.Vector})
	}
	return hits, nil
}

func stringifyID(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		// qdrant may return numeric ids; not expected for UUID strings
		return fmt.Sprintf("%v", v)
	default:
		s := fmt.Sprintf("%v", v)
		if _, err := uuid.Parse(s); err == nil {
			return s
		}
		return s
	}
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant unhealthy: %d", resp.StatusCode)
	}
	return nil
}
