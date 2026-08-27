package interactionwriter

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
)

type Kind int

const (
	KindUpsertLike Kind = iota
	KindDeleteLike
	KindInsertSave
	KindDeleteSave
	KindInsertShare
)

type Job struct {
	Kind   Kind
	UserID uuid.UUID
	PostID uuid.UUID
	IsLike bool // for KindUpsertLike
}

// Writer persists engagement rows to Postgres via a bounded worker pool.
type Writer struct {
	db          *db.Store
	ch          chan Job
	wg          sync.WaitGroup
	dropped     int64
	lastDropLog time.Time
	mu          sync.Mutex
}

func New(store *db.Store, workers, queueSize int) *Writer {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	w := &Writer{
		db: store,
		ch: make(chan Job, queueSize),
	}
	for i := 0; i < workers; i++ {
		w.wg.Add(1)
		go w.loop()
	}
	return w
}

func (w *Writer) loop() {
	defer w.wg.Done()
	for j := range w.ch {
		ctx := context.Background()
		if err := w.apply(ctx, j); err != nil {
			log.Printf("interactionwriter: %v", err)
		}
	}
}

func (w *Writer) apply(ctx context.Context, j Job) error {
	switch j.Kind {
	case KindUpsertLike:
		return w.db.UpsertLike(ctx, j.UserID, j.PostID, j.IsLike)
	case KindDeleteLike:
		return w.db.DeleteLike(ctx, j.UserID, j.PostID)
	case KindInsertSave:
		return w.db.InsertSave(ctx, j.UserID, j.PostID)
	case KindDeleteSave:
		return w.db.DeleteSave(ctx, j.UserID, j.PostID)
	case KindInsertShare:
		inserted, err := w.db.InsertShare(ctx, j.UserID, j.PostID)
		if err != nil {
			return err
		}
		if inserted {
			_, err = w.db.IncrementShare(ctx, j.PostID)
			return err
		}
		return nil
	default:
		return nil
	}
}

// Enqueue schedules a durable write. Queue full → drop (Redis eng remains hot source of truth).
func (w *Writer) Enqueue(j Job) {
	if w == nil {
		return
	}
	select {
	case w.ch <- j:
	default:
		w.mu.Lock()
		w.dropped++
		n := w.dropped
		now := time.Now()
		shouldLog := now.Sub(w.lastDropLog) >= time.Second
		if shouldLog {
			w.lastDropLog = now
		}
		w.mu.Unlock()
		if shouldLog {
			log.Printf("interactionwriter: queue full, shedding excess jobs (total dropped=%d)", n)
		}
	}
}

func (w *Writer) Close() {
	if w == nil {
		return
	}
	close(w.ch)
	w.wg.Wait()
}
