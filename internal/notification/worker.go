package notification

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Worker struct {
	repository *Repository
	mailer     Mailer
	interval   time.Duration
}

func NewWorker(repository *Repository, mailer Mailer, interval time.Duration) *Worker {
	return &Worker{repository: repository, mailer: mailer, interval: interval}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.repository == nil || w.mailer == nil {
		return
	}
	if w.interval <= 0 {
		w.interval = 15 * time.Second
	}
	w.DeliverPending(ctx, 20)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.DeliverPending(ctx, 20)
		}
	}
}

func (w *Worker) DeliverPending(ctx context.Context, limit int) {
	if w == nil || w.repository == nil || w.mailer == nil {
		return
	}
	items, err := w.repository.ClaimPending(ctx, limit)
	if err != nil {
		slog.Warn("email notification claim failed", "error", err)
		return
	}
	for _, item := range items {
		if err := w.mailer.Send(ctx, item.Message); err != nil {
			if errors.Is(err, ErrDeliveryDisabled) {
				_ = w.repository.MarkFailed(ctx, item.ID, 5, err)
				return
			}
			_ = w.repository.MarkFailed(ctx, item.ID, 1, err)
			continue
		}
		if err := w.repository.MarkSent(ctx, item.ID); err != nil {
			slog.Warn("email notification sent-state failed", "error", err)
		}
	}
}
