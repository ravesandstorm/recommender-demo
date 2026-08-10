package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"unicode/utf8"

	fastembed "github.com/anush008/fastembed-go"
)

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Dim        int         `json:"dim"`
}

type server struct {
	model *fastembed.FlagEmbedding
	mu    sync.Mutex
}

func main() {
	cacheDir := envOr("CACHE_DIR", "/models")
	addr := envOr("ADDR", ":8081")

	showProgress := false
	log.Printf("initializing fastembed model BGESmallENV15 (cache=%s)", cacheDir)
	model, err := fastembed.NewFlagEmbedding(&fastembed.InitOptions{
		Model:               fastembed.BGESmallENV15,
		CacheDir:            cacheDir,
		MaxLength:           512,
		ShowDownloadProgress: &showProgress,
	})
	if err != nil {
		log.Fatalf("failed to init embedding model: %v", err)
	}
	defer model.Destroy()

	s := &server{model: model}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /embed", s.handleEmbed)

	log.Printf("embedding service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"model":  string(fastembed.BGESmallENV15),
		"dim":    384,
	})
}

func (s *server) handleEmbed(w http.ResponseWriter, r *http.Request) {
	var req embedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if len(req.Texts) == 0 {
		http.Error(w, "texts must be a non-empty array", http.StatusBadRequest)
		return
	}

	// sugarme/tokenizer panics on some long inputs; keep well under model MaxLength.
	const maxRunes = 400
	texts := make([]string, 0, len(req.Texts))
	for _, t := range req.Texts {
		t = truncateRunes(t, maxRunes)
		if t == "" {
			t = " "
		}
		texts = append(texts, t)
	}

	embeddings := make([][]float32, 0, len(texts))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range texts {
		var (
			one [][]float32
			err error
		)
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					err = errPanic(rec)
				}
			}()
			one, err = s.model.Embed([]string{t}, 1)
		}()
		if err != nil {
			http.Error(w, "embedding failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if len(one) != 1 {
			http.Error(w, "embedding failed: unexpected batch size", http.StatusInternalServerError)
			return
		}
		embeddings = append(embeddings, one[0])
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(embedResponse{
		Embeddings: embeddings,
		Dim:        384,
	})
}

func errPanic(rec any) error {
	return &panicError{v: rec}
}

type panicError struct{ v any }

func (e *panicError) Error() string {
	return "panic during embed: " + toString(e.v)
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
