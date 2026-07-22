package limits

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type scriptedQueryRower struct {
	t       *testing.T
	rows    []pgx.Row
	queries []string
}

func (query *scriptedQueryRower) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	query.t.Helper()
	query.queries = append(query.queries, sql)
	if len(query.rows) == 0 {
		query.t.Fatal("unexpected QueryRow call")
	}
	row := query.rows[0]
	query.rows = query.rows[1:]
	return row
}

type scriptedRow struct {
	scan func(...any) error
}

func (row scriptedRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

func entitlementRow(limit *int, status string) pgx.Row {
	return scriptedRow{scan: func(dest ...any) error {
		*dest[0].(**int) = limit
		*dest[1].(*string) = status
		return nil
	}}
}

func boolRow(value bool) pgx.Row {
	return scriptedRow{scan: func(dest ...any) error {
		*dest[0].(*bool) = value
		return nil
	}}
}

func usageRow(used int) pgx.Row {
	return scriptedRow{scan: func(dest ...any) error {
		*dest[0].(*int) = used
		return nil
	}}
}

func stringRow(value string) pgx.Row {
	return scriptedRow{scan: func(dest ...any) error {
		*dest[0].(*string) = value
		return nil
	}}
}

func errorRow(err error) pgx.Row {
	return scriptedRow{scan: func(...any) error { return err }}
}

func TestPublicationCapacityConsumesDurableLedgerUnit(t *testing.T) {
	limit := 5
	query := &scriptedQueryRower{t: t, rows: []pgx.Row{
		entitlementRow(&limit, "active"),
		boolRow(false),
		usageRow(4),
		stringRow("consumption-id"),
	}}

	capacity, err := LockPublicationCapacity(context.Background(), query, "project")
	if err != nil {
		t.Fatalf("LockPublicationCapacity() error = %v", err)
	}
	if err := capacity.Consume(context.Background(), query, "project", PublicationSourceSocialPost, "source"); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if len(query.queries) != 4 {
		t.Fatalf("expected lock, idempotency, usage and insert queries, got %d", len(query.queries))
	}
	if !strings.Contains(query.queries[0], "FOR UPDATE") {
		t.Fatalf("first query must lock the entitlement row: %s", query.queries[0])
	}
	if !strings.Contains(query.queries[1], "publication_consumptions") || !strings.Contains(query.queries[2], "count(*)") {
		t.Fatalf("capacity must be based only on the durable ledger: %#v", query.queries)
	}
	if !strings.Contains(query.queries[3], "INSERT INTO publication_consumptions") {
		t.Fatalf("final query must persist consumption: %s", query.queries[3])
	}
}

func TestPublicationCapacityRetryIsIdempotentAtLimit(t *testing.T) {
	limit := 1
	query := &scriptedQueryRower{t: t, rows: []pgx.Row{
		entitlementRow(&limit, "active"),
		boolRow(true),
	}}
	capacity, err := LockPublicationCapacity(context.Background(), query, "project")
	if err != nil {
		t.Fatalf("LockPublicationCapacity() error = %v", err)
	}
	if err := capacity.Consume(context.Background(), query, "project", PublicationSourceContentVariant, "source"); err != nil {
		t.Fatalf("idempotent Consume() error = %v", err)
	}
	if len(query.queries) != 2 {
		t.Fatalf("idempotent consumption must not recount or insert, got %d queries", len(query.queries))
	}
}

func TestPublicationCapacityRejectsInactiveOrMissingEntitlement(t *testing.T) {
	for _, test := range []struct {
		name string
		row  pgx.Row
	}{
		{name: "past due", row: entitlementRow(nil, "past_due")},
		{name: "cancelled", row: entitlementRow(nil, "cancelled")},
		{name: "missing", row: errorRow(pgx.ErrNoRows)},
	} {
		t.Run(test.name, func(t *testing.T) {
			query := &scriptedQueryRower{t: t, rows: []pgx.Row{test.row}}
			_, err := LockPublicationCapacity(context.Background(), query, "project")
			if !errors.Is(err, ErrEntitlementInactive) {
				t.Fatalf("expected ErrEntitlementInactive, got %v", err)
			}
		})
	}
}

func TestPublicationCapacityEnforcesLimitAndNewsletterAdoption(t *testing.T) {
	limit := 2
	query := &scriptedQueryRower{t: t, rows: []pgx.Row{
		entitlementRow(&limit, "trial"),
		boolRow(false),
		usageRow(2),
	}}
	capacity, err := LockPublicationCapacity(context.Background(), query, "project")
	if err != nil {
		t.Fatalf("LockPublicationCapacity() error = %v", err)
	}
	err = capacity.Consume(context.Background(), query, "project", PublicationSourceNewsletterDelivery, "source")
	if !errors.Is(err, ErrPublicationLimitReached) {
		t.Fatalf("expected ErrPublicationLimitReached, got %v", err)
	}

	adoptionQuery := &scriptedQueryRower{t: t, rows: []pgx.Row{boolRow(true)}}
	capacity = PublicationCapacity{limit: &limit, validated: true}
	if err := capacity.RequireAvailableForNewsletterItem(context.Background(), adoptionQuery, "project", "item"); err != nil {
		t.Fatalf("existing newsletter variant must be adoptable: %v", err)
	}
	if len(adoptionQuery.queries) != 1 {
		t.Fatalf("newsletter adoption must not count another unit, got %d queries", len(adoptionQuery.queries))
	}
}

func TestPublicationCapacityRejectsUnknownSource(t *testing.T) {
	query := &scriptedQueryRower{t: t}
	err := (PublicationCapacity{}).Consume(context.Background(), query, "project", "unknown", "source")
	if !errors.Is(err, ErrInvalidPublicationSource) {
		t.Fatalf("expected ErrInvalidPublicationSource, got %v", err)
	}
	err = (PublicationCapacity{}).Consume(context.Background(), query, "project", PublicationSourceSocialPost, "source")
	if !errors.Is(err, ErrEntitlementInactive) {
		t.Fatalf("unvalidated capacity must not consume: %v", err)
	}
}
