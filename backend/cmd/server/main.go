package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/embedclient"
	"github.com/recsys/backend/internal/handlers"
	"github.com/recsys/backend/internal/migrate"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer store.Pool.Close()

	if err := migrate.Up(ctx, store.Pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied")

	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		log.Fatalf("redis: %v", err)
	}

	qdrant := qdrantclient.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.VectorDim)
	if err := qdrant.EnsureCollection(ctx); err != nil {
		log.Fatalf("qdrant collection: %v", err)
	}

	embed := embedclient.New(cfg.EmbeddingURL)

	api := &handlers.API{
		Cfg:    cfg,
		DB:     store,
		Redis:  rdb,
		Qdrant: qdrant,
		Embed:  embed,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000", "http://localhost:3001"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "X-User-ID"},
		AllowCredentials: true,
	}))
	r.Mount("/", api.Routes())

	srv := &http.Server{Addr: cfg.Addr, Handler: r}
	go func() {
		log.Printf("api listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
