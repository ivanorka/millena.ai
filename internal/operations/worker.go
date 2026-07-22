package operations

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	defaultInterval  = 2 * time.Second
	defaultBatchSize = 50
)

type dueProcessor interface {
	ProcessDue(context.Context, int) (BatchResult, error)
}

type Worker struct {
	processor dueProcessor
	interval  time.Duration
	batchSize int
}

func NewWorker(repository *Repository, interval time.Duration) *Worker {
	return newWorker(repository, interval, defaultBatchSize)
}

func newWorker(processor dueProcessor, interval time.Duration, batchSize int) *Worker {
	if interval <= 0 {
		interval = defaultInterval
	}
	if batchSize < 1 {
		batchSize = defaultBatchSize
	}
	return &Worker{processor: processor, interval: interval, batchSize: batchSize}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.processor == nil {
		return
	}
	w.process(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *Worker) process(ctx context.Context) {
	result, err := w.processor.ProcessDue(ctx, w.batchSize)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			slog.Warn("sandbox operations worker failed", "error", err)
		}
		return
	}
	if result.Total() == 0 {
		return
	}
	slog.Info("sandbox operations processed",
		"automations", result.AutomationsRun,
		"publicationsSucceeded", result.PublicationsSucceeded,
		"publicationsFailed", result.PublicationsFailed,
		"newslettersSent", result.NewslettersSent,
		"newslettersFailed", result.NewslettersFailed,
	)
}
