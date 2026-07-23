package limits

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrEntitlementInactive      = errors.New("project entitlement is not active")
	ErrPublicationLimitReached  = errors.New("monthly publication limit has been reached")
	ErrInvalidPublicationSource = errors.New("invalid publication consumption source")
)

const (
	PublicationSourceContentVariant     = "content_variant"
	PublicationSourceSocialPost         = "social_post"
	PublicationSourceNewsletterDelivery = "newsletter_delivery"
)

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PublicationCapacity is an organization entitlement row lock held by the
// caller's transaction. All quota checks across every project in the tenant
// serialize on this lock until that transaction commits or rolls back.
type PublicationCapacity struct {
	limit     *int
	validated bool
}

// LockPublicationCapacity locks and validates the project's entitlement. The
// lock is held until the surrounding transaction commits or rolls back.
func LockPublicationCapacity(ctx context.Context, query QueryRower, projectID string) (PublicationCapacity, error) {
	var limit *int
	var status string
	err := query.QueryRow(ctx, `
		SELECT entitlement.monthly_publication_limit, entitlement.status
		FROM projects AS project
		JOIN organization_entitlements AS entitlement
		  ON entitlement.organization_id=project.organization_id
		WHERE project.id=$1::uuid
		FOR UPDATE OF entitlement`, projectID).Scan(&limit, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicationCapacity{}, ErrEntitlementInactive
	}
	if err != nil {
		return PublicationCapacity{}, err
	}
	if status != "active" && status != "trial" {
		return PublicationCapacity{}, ErrEntitlementInactive
	}
	return PublicationCapacity{limit: limit, validated: true}, nil
}

// RequireAvailable checks the durable ledger for the current UTC calendar
// month. It intentionally does not consume a slot; callers that create a
// publishing record must subsequently call Consume in the same transaction.
func (capacity PublicationCapacity) RequireAvailable(ctx context.Context, query QueryRower, projectID string) error {
	if !capacity.validated {
		return ErrEntitlementInactive
	}
	return capacity.requireAvailable(ctx, query, projectID)
}

// RequireAvailableForNewsletterItem treats a ledger entry for a newsletter
// variant of the same item as an already-consumed unit. This preserves the
// transition-aware API used when the dedicated delivery queue adopts a
// scheduled newsletter variant.
func (capacity PublicationCapacity) RequireAvailableForNewsletterItem(ctx context.Context, query QueryRower, projectID, itemID string) error {
	if !capacity.validated {
		return ErrEntitlementInactive
	}
	var alreadyConsumed bool
	err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM publication_consumptions AS consumption
			JOIN content_variants AS variant
			  ON consumption.source_type = 'content_variant'
			 AND consumption.source_id = variant.id
			WHERE consumption.project_id = $1::uuid
			  AND consumption.billing_month = date_trunc('month', now() AT TIME ZONE 'UTC')::date
			  AND variant.content_item_id = $2::uuid
			  AND variant.channel = 'newsletter'
		)`, projectID, itemID).Scan(&alreadyConsumed)
	if err != nil {
		return err
	}
	if alreadyConsumed {
		return nil
	}
	return capacity.requireAvailable(ctx, query, projectID)
}

func (capacity PublicationCapacity) requireAvailable(ctx context.Context, query QueryRower, projectID string) error {
	if capacity.limit == nil {
		return nil
	}
	var used int
	err := query.QueryRow(ctx, `
		SELECT count(*)::int
		FROM publication_consumptions AS consumption
		JOIN projects AS consumed_project ON consumed_project.id=consumption.project_id
		JOIN projects AS active_project
		  ON active_project.id=$1::uuid
		 AND active_project.organization_id=consumed_project.organization_id
		WHERE consumption.billing_month=date_trunc('month', now() AT TIME ZONE 'UTC')::date`, projectID).Scan(&used)
	if err != nil {
		return err
	}
	if used >= *capacity.limit {
		return ErrPublicationLimitReached
	}
	return nil
}

// Consume writes one durable, idempotent unit for a source in the current UTC
// calendar month. Callers may safely invoke it for retries and reschedules: a
// source can consume at most once per month. The mutation and this insert must
// share the transaction that owns the PublicationCapacity lock.
func (capacity PublicationCapacity) Consume(ctx context.Context, query QueryRower, projectID, sourceType, sourceID string) error {
	if !validPublicationSource(sourceType) {
		return ErrInvalidPublicationSource
	}
	if !capacity.validated {
		return ErrEntitlementInactive
	}
	var alreadyConsumed bool
	err := query.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM publication_consumptions
			WHERE project_id = $1::uuid
			  AND source_type = $2
			  AND source_id = $3::uuid
			  AND billing_month = date_trunc('month', now() AT TIME ZONE 'UTC')::date
		)`, projectID, sourceType, sourceID).Scan(&alreadyConsumed)
	if err != nil {
		return err
	}
	if alreadyConsumed {
		return nil
	}
	if err := capacity.requireAvailable(ctx, query, projectID); err != nil {
		return err
	}
	var consumptionID string
	err = query.QueryRow(ctx, `
		INSERT INTO publication_consumptions (
			project_id, source_type, source_id, billing_month
		)
		VALUES (
			$1::uuid, $2, $3::uuid,
			date_trunc('month', now() AT TIME ZONE 'UTC')::date
		)
		ON CONFLICT (project_id, source_type, source_id, billing_month) DO NOTHING
		RETURNING id::text`, projectID, sourceType, sourceID).Scan(&consumptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func validPublicationSource(sourceType string) bool {
	return sourceType == PublicationSourceContentVariant ||
		sourceType == PublicationSourceSocialPost ||
		sourceType == PublicationSourceNewsletterDelivery
}

// RequirePublicationCapacity remains the common preflight guard for callers
// that do not yet have a source ID. The publishing mutation must still call
// PublicationCapacity.Consume before committing.
func RequirePublicationCapacity(ctx context.Context, query QueryRower, projectID string) error {
	capacity, err := LockPublicationCapacity(ctx, query, projectID)
	if err != nil {
		return err
	}
	return capacity.RequireAvailable(ctx, query, projectID)
}
