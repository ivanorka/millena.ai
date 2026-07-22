package social

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Worker struct {
	repository *Repository
	interval   time.Duration
}

func NewWorker(repository *Repository, interval time.Duration) *Worker {
	return &Worker{repository: repository, interval: interval}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.repository == nil {
		return
	}
	if w.interval <= 0 {
		w.interval = 2 * time.Second
	}

	w.publishDue(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.publishDue(ctx)
		}
	}
}

func (w *Worker) publishDue(ctx context.Context) {
	count, err := w.repository.PublishDueSandbox(ctx, 50)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("sandbox social worker failed", "error", err)
		return
	}
	if count > 0 {
		slog.Info("sandbox social posts published", "count", count)
	}
}
