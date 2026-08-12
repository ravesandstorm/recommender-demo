package viewwriter

import (
	"context"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
)

type job struct {
	userID  uuid.UUID
	postIDs []uuid.UUID
}

// Writer persists feed impressions to Postgres via a bounded worker pool.
type Writer struct {
	db      *db.Store
	ch      chan job
	wg      sync.WaitGroup
	dropped int64
	mu      sync.Mutex
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
		ch: make(chan job, queueSize),
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
		if err := w.db.MarkViewed(ctx, j.userID, j.postIDs); err != nil {
			log.Printf("viewwriter: mark viewed: %v", err)
		}
	}
}

// Enqueue schedules a durable write. If the queue is full, the job is dropped
// (Redis seen remains source of truth for the hot path).
func (w *Writer) Enqueue(userID uuid.UUID, postIDs []uuid.UUID) {
	if w == nil || len(postIDs) == 0 {
		return
	}
	// Copy IDs so callers can reuse their slice.
	ids := make([]uuid.UUID, len(postIDs))
	copy(ids, postIDs)
	select {
	case w.ch <- job{userID: userID, postIDs: ids}:
	default:
		w.mu.Lock()
		w.dropped++
		n := w.dropped
		w.mu.Unlock()
		log.Printf("viewwriter: queue full, dropped job (total dropped=%d)", n)
	}
}

// Close stops workers after draining the queue.
func (w *Writer) Close() {
	if w == nil {
		return
	}
	close(w.ch)
	w.wg.Wait()
}
