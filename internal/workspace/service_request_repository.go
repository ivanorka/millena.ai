package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) UpdateServiceRequest(
	ctx context.Context,
	projectID, requestID, actorID string,
	input ServiceRequestUpdateInput,
) (ServiceRequest, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ServiceRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var previous ServiceRequest
	err = tx.QueryRow(ctx, `
		SELECT id::text, project_id::text, request_type, status, summary, metadata,
		       created_by::text, created_at, updated_at
		FROM service_requests
		WHERE project_id = $1::uuid AND id = $2::uuid
		FOR UPDATE`, projectID, requestID).Scan(
		&previous.ID, &previous.ProjectID, &previous.RequestType, &previous.Status,
		&previous.Summary, &previous.Metadata, &previous.CreatedBy,
		&previous.CreatedAt, &previous.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceRequest{}, ErrNotFound
	}
	if err != nil {
		return ServiceRequest{}, err
	}

	summary := previous.Summary
	if input.Summary != nil {
		summary = *input.Summary
	}
	metadata := previous.Metadata
	if input.Metadata != nil {
		metadata = *input.Metadata
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	priority, err := serviceRequestPriority(ctx, tx, projectID)
	if err != nil {
		return ServiceRequest{}, err
	}
	metadata["priority"] = priority

	var request ServiceRequest
	err = tx.QueryRow(ctx, `
		UPDATE service_requests
		SET status = $3, summary = $4, metadata = $5::jsonb, updated_at = now()
		WHERE project_id = $1::uuid AND id = $2::uuid
		RETURNING id::text, project_id::text, request_type, status, summary, metadata,
		          created_by::text, created_at, updated_at`,
		projectID, requestID, input.Status, summary, metadata).Scan(
		&request.ID, &request.ProjectID, &request.RequestType, &request.Status,
		&request.Summary, &request.Metadata, &request.CreatedBy,
		&request.CreatedAt, &request.UpdatedAt,
	)
	if err != nil {
		return ServiceRequest{}, err
	}

	if err := recordAudit(ctx, tx, projectID, actorID, "service_request.updated", "service_request", &request.ID, map[string]any{
		"previousStatus":  previous.Status,
		"status":          request.Status,
		"summaryChanged":  input.Summary != nil && previous.Summary != request.Summary,
		"metadataChanged": input.Metadata != nil,
	}); err != nil {
		return ServiceRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ServiceRequest{}, err
	}
	return request, nil
}
