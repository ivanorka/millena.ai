.PHONY: run test fmt vet compose-up compose-down db-migrate-up db-migrate-down

run:
	go run ./cmd/api

test:
	go test ./...

fmt:
	gofmt -w $$(find cmd internal -name '*.go')

vet:
	go vet ./...

compose-up:
	docker compose up --build

compose-down:
	docker compose down

db-migrate-up:
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000001_initial_schema.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000002_project_app_state.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000003_social_sandbox.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000004_auth_permissions_calendar.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000005_content_strategy_ai.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000006_operational_workspace.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000007_project_assets.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000008_project_personas.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000009_content_item_assets.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000010_social_publication_cascade.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000011_newsletter_schedule_consistency.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000012_publication_consumptions.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000013_remove_unimplemented_sso_feature.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000014_timezone_aware_seed_anchors.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000015_starter_ten_publications.up.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000016_registration_plan_choices.up.sql

db-migrate-down:
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000016_registration_plan_choices.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000015_starter_ten_publications.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000014_timezone_aware_seed_anchors.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000013_remove_unimplemented_sso_feature.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000012_publication_consumptions.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000011_newsletter_schedule_consistency.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000010_social_publication_cascade.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000009_content_item_assets.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000008_project_personas.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000007_project_assets.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000006_operational_workspace.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000005_content_strategy_ai.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000004_auth_permissions_calendar.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000003_social_sandbox.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000002_project_app_state.down.sql
	psql "$$DATABASE_URL" -1 -v ON_ERROR_STOP=1 -f migrations/000001_initial_schema.down.sql
