package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrNotFound = errors.New("authentication record not found")
var ErrEmailConflict = errors.New("email already exists")
var ErrSlugConflict = errors.New("project slug already exists")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	if pool == nil {
		return nil
	}
	return &Repository{pool: pool}
}

func (r *Repository) EnsureMPRWorkspace(ctx context.Context, email, displayName, password string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var projectID string
	err = tx.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, settings)
		VALUES ('MPR Grupa', 'millena-demo', 'hr', '{"clientType":"enterprise","brand":"MPR Grupa"}'::jsonb)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name, updated_at = now()
		RETURNING id::text`,
	).Scan(&projectID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_app_states (project_id)
		VALUES ($1::uuid)
		ON CONFLICT (project_id) DO NOTHING`, projectID); err != nil {
		return err
	}

	var userID string
	var existingHash *string
	err = tx.QueryRow(ctx, `
		SELECT id::text, password_hash
		FROM users
		WHERE lower(email) = lower($1)`, email).Scan(&userID, &existingHash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			INSERT INTO users (email, display_name, status, password_hash)
			VALUES (lower($1), $2, 'active', $3)
			RETURNING id::text`, email, displayName, string(passwordHash)).Scan(&userID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if existingHash == nil || *existingHash == "" {
		if _, err := tx.Exec(ctx, `
			UPDATE users SET password_hash = $2, display_name = $3, status = 'active', updated_at = now()
			WHERE id = $1::uuid`, userID, string(passwordHash), displayName); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role, permissions, status)
		VALUES ($1::uuid, $2::uuid, 'owner', '{"*":true}'::jsonb, 'active')
		ON CONFLICT (project_id, user_id) DO UPDATE
		SET role = 'owner', permissions = '{"*":true}'::jsonb, status = 'active'`, projectID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_entitlements (
			project_id, plan_code, status, seat_limit, monthly_publication_limit, storage_limit_bytes, features
		)
		VALUES ($1::uuid, 'unlimited', 'active', NULL, NULL, NULL,
			'{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":true,"socialChannels":"all","whiteLabel":true}'::jsonb)
		ON CONFLICT (project_id) DO UPDATE
		SET plan_code = 'unlimited', status = 'active', seat_limit = NULL,
		    monthly_publication_limit = NULL, storage_limit_bytes = NULL,
		    features = EXCLUDED.features, updated_at = now()`, projectID); err != nil {
		return err
	}
	if err := seedCalendar(ctx, tx, projectID, userID); err != nil {
		return err
	}
	if err := seedMPRStrategyAndContent(ctx, tx, projectID, userID); err != nil {
		return err
	}
	if err := seedOperationalWorkspace(ctx, tx, projectID, userID, mprOperationalWorkspaceSeed()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func seedMPRStrategyAndContent(ctx context.Context, tx pgx.Tx, projectID, userID string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_strategies (
			project_id, mode, six_month_goal, primary_goals, priority_topics, audience,
			audience_problem, brand_message, proof_points, forbidden_topics,
			success_metrics, tone, updated_by
		)
		VALUES ($1::uuid, 'questions',
			'Povećati broj kvalitetnih poslovnih upita, vidljivost stručnjaka i kontinuitet komunikacije.',
			ARRAY['Novi poslovni upiti', 'Ugled brenda', 'Vidljivost stručnjaka'],
			ARRAY['Korporativne komunikacije', 'Ljudi i kultura', 'Studije slučaja', 'Događaji'],
			'Direktorice marketinga, komunikacijski timovi i uprave srednjih i velikih organizacija.',
			'Složene poslovne teme treba pretvoriti u jasne, vjerodostojne i korisne poruke.',
			'MPR Grupa spaja strateško razmišljanje, provjerene činjenice i izvedbu koja gradi povjerenje.',
			'Studije slučaja, izjave klijenata, rezultati kampanja, stručni komentari i istraživanja.',
			'Neprovjerene brojke, povjerljivi podaci, politički stavovi i obećanja bez dokaza.',
			'Kvalitetni upiti, doseg stručnjaka, spremanja sadržaja, rast newsletter publike i konverzije.',
			'Stručno i samouvjereno, ali pristupačno; jasno, konkretno i bez generičkih marketinških fraza.',
			$2::uuid)
		ON CONFLICT (project_id) DO NOTHING`, projectID, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, summary, body, channels,
			scheduled_for, source, metadata, seed_key
		)
		VALUES
			($1::uuid, $2::uuid, 'social', 'in_review', 'Kako povjerenje nastaje prije prve kampanje', 'LinkedIn objava s jasnim stavom i pitanjem za komunikacijske timove.', 'Povjerenje se ne gradi jednom velikom kampanjom. Gradi se svakim jasnim odgovorom, svakom provjerenom činjenicom i svakim trenutkom u kojem organizacija pokaže da sluša.', ARRAY['linkedin'], now() + interval '1 day', 'ai', '{"seeded":true}'::jsonb, 'mpr-social-trust'),
			($1::uuid, $2::uuid, 'blog', 'draft', 'Pet trendova koji mijenjaju odnose s javnošću', 'Analitički članak za web s primjerima primjene.', 'Komunikacijski timovi rade u okruženju u kojem se očekivanja publike mijenjaju brže od godišnjih planova.', ARRAY['blog'], NULL, 'ai', '{"seeded":true}'::jsonb, 'mpr-blog-trends'),
			($1::uuid, $2::uuid, 'newsletter', 'scheduled', 'Tjedni pregled: ljudi, projekti i ideje', 'Urednički odabir najboljih uvida iz projekta.', 'Ovaj tjedan izdvajamo lekciju iz krizne komunikacije, pogled iza kulisa produkcije i tri pitanja za jasniju poruku.', ARRAY['newsletter'], date_trunc('week', now()) + interval '4 days 10 hours', 'ai', '{"seeded":true}'::jsonb, 'mpr-newsletter-weekly'),
			($1::uuid, $2::uuid, 'press_release', 'approved', 'MPR Grupa širi tim za integrirane komunikacije', 'Priopćenje s naglaskom na novu ekspertizu i korist za klijente.', 'MPR Grupa proširila je tim za integrirane komunikacije kako bi klijentima povezala strateško savjetovanje, sadržaj i digitalnu distribuciju.', ARRAY['media','blog'], now() + interval '3 days 11 hours', 'manual', '{"seeded":true}'::jsonb, 'mpr-press-team'),
			($1::uuid, $2::uuid, 'case_study', 'in_review', 'Od stručnog događaja do mjesec dana relevantnog sadržaja', 'Studija slučaja kroz izazov, pristup i rezultat.', 'Vrijedni uvidi s događaja pretvoreni su u povezani niz objava, članak i newsletter.', ARRAY['blog','linkedin','newsletter'], NULL, 'manual', '{"seeded":true}'::jsonb, 'mpr-case-event'),
			($1::uuid, $2::uuid, 'event', 'scheduled', 'Komunikacije koje grade povjerenje — otvoreni studio', 'Najava stručnog susreta za klijente i komunikacijsku zajednicu.', 'Otvaramo studio za razgovor o komunikaciji koja ostaje vjerodostojna i kada se kanali i očekivanja brzo mijenjaju.', ARRAY['linkedin','newsletter'], now() + interval '6 days 9 hours', 'manual', '{"seeded":true}'::jsonb, 'mpr-event-studio')
		ON CONFLICT (project_id, seed_key) WHERE seed_key IS NOT NULL DO NOTHING`, projectID, userID)
	return err
}

func seedCalendar(ctx context.Context, tx pgx.Tx, projectID, userID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO calendar_items (project_id, created_by, title, summary, channel, status, scheduled_for, seed_key)
		VALUES
			($1::uuid, $2::uuid, 'Nova studija slučaja', 'Rezultati kampanje i tri ključna uvida za klijente.', 'linkedin', 'published', date_trunc('week', now()) + interval '9 hours 30 minutes', 'mpr-case-study'),
			($1::uuid, $2::uuid, 'Iza kulisa novog studija', 'Fotografije tima, prostora i lokalnih autora.', 'instagram', 'in_review', date_trunc('week', now()) + interval '1 day 12 hours', 'mpr-behind-scenes'),
			($1::uuid, $2::uuid, 'Savjet stručnjaka: komunikacija u krizi', 'Millena prijedlog koji čeka uredničku odluku.', 'linkedin', 'suggestion', date_trunc('week', now()) + interval '2 days 9 hours', 'mpr-expert-tip'),
			($1::uuid, $2::uuid, 'Pet trendova koji mijenjaju odnose s javnošću', 'Dugi format za blog i newsletter distribuciju.', 'blog', 'scheduled', date_trunc('week', now()) + interval '3 days 12 hours 30 minutes', 'mpr-pr-trends'),
			($1::uuid, $2::uuid, 'Tjedni pregled: ljudi, projekti i ideje', 'Automatska zbirka najboljih sadržaja iz projekta.', 'newsletter', 'scheduled', date_trunc('week', now()) + interval '4 days 10 hours', 'mpr-weekly-review'),
			($1::uuid, $2::uuid, 'Projekt u fokusu', 'Kratki pregled projekta s poveznicom na studiju slučaja.', 'facebook', 'scheduled', date_trunc('week', now()) + interval '16 hours', 'mpr-project-spotlight')
		ON CONFLICT (project_id, seed_key) WHERE seed_key IS NOT NULL DO NOTHING`, projectID, userID)
	return err
}

func (r *Repository) Register(ctx context.Context, input RegisterInput) (User, ProjectAccess, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		return User{}, ProjectAccess{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return User{}, ProjectAccess{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var user User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, display_name, status, password_hash)
		VALUES (lower($1), $2, 'active', $3)
		RETURNING id::text, email, display_name, status, last_login_at, created_at, updated_at`,
		input.Email, input.DisplayName, string(passwordHash)).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Status, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if postgresCode(err) == "23505" {
		return User{}, ProjectAccess{}, ErrEmailConflict
	}
	if err != nil {
		return User{}, ProjectAccess{}, err
	}

	var access ProjectAccess
	err = tx.QueryRow(ctx, `
		INSERT INTO projects (name, slug, default_locale, settings)
		VALUES ($1, $2, 'hr', jsonb_build_object('clientType', 'enterprise', 'brand', $1::text))
		RETURNING id::text, name, slug, default_locale`, input.OrganizationName, input.ProjectSlug).Scan(
		&access.ProjectID, &access.ProjectName, &access.ProjectSlug, &access.DefaultLocale)
	if postgresCode(err) == "23505" {
		return User{}, ProjectAccess{}, ErrSlugConflict
	}
	if err != nil {
		return User{}, ProjectAccess{}, err
	}
	access.Role = "owner"
	access.Permissions = map[string]any{"*": true}
	access.Entitlement = unlimitedEntitlement()

	if _, err := tx.Exec(ctx, `
		INSERT INTO project_members (project_id, user_id, role, permissions, status)
		VALUES ($1::uuid, $2::uuid, 'owner', '{"*":true}'::jsonb, 'active')`, access.ProjectID, user.ID); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_entitlements (project_id, plan_code, status, features)
		VALUES ($1::uuid, 'unlimited', 'active', $2::jsonb)`, access.ProjectID, unlimitedFeaturesJSON); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_app_states (project_id) VALUES ($1::uuid)`, access.ProjectID); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if err := seedCalendar(ctx, tx, access.ProjectID, user.ID); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if err := seedNewTenantStrategyAndContent(ctx, tx, access.ProjectID, user.ID, input.OrganizationName); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if err := seedOperationalWorkspace(ctx, tx, access.ProjectID, user.ID, newTenantOperationalWorkspaceSeed(input.OrganizationName)); err != nil {
		return User{}, ProjectAccess{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, ProjectAccess{}, err
	}
	return user, access, nil
}

func seedNewTenantStrategyAndContent(ctx context.Context, tx pgx.Tx, projectID, userID, organizationName string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_strategies (
			project_id, mode, six_month_goal, primary_goals, priority_topics, audience,
			brand_message, proof_points, forbidden_topics, success_metrics, tone, updated_by
		)
		VALUES ($1::uuid, 'questions',
			'Izgraditi prepoznatljivu i dosljednu komunikaciju u sljedećih šest mjeseci.',
			ARRAY['Ugled brenda', 'Kvalitetni upiti'], ARRAY['Stručnost', 'Ljudi', 'Rezultati'],
			'Klijenti i partneri kojima je važna stručna i pouzdana suradnja.',
			$3::text || ' pomaže publici jasnim, korisnim i provjerenim informacijama.',
			'Primjeri iz prakse, iskustvo tima i provjereni rezultati.',
			'Povjerljive informacije, neprovjerene brojke i tvrdnje bez izvora.',
			'Kvalitetni upiti, angažman i rast vlastite publike.',
			'Stručno, ljudski, jasno i konkretno.', $2::uuid)`, projectID, userID, organizationName); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO content_items (
			project_id, author_id, kind, status, title, summary, body, channels, source, metadata, seed_key
		)
		VALUES
			($1::uuid, $2::uuid, 'source', 'draft', 'Ideje i činjenice za prvi sadržaj', 'Početni izvorni materijal za tim.', 'Dodajte ključne činjenice, izjave i poveznice koje Millena smije koristiti.', ARRAY[]::text[], 'manual', '{"seeded":true}'::jsonb, 'starter-source'),
			($1::uuid, $2::uuid, 'social', 'draft', $3::text || ': upoznajte naš pristup', 'Prva LinkedIn tema projekta.', $3::text || ' spaja stručnost, jasan proces i suradnju usmjerenu na rezultat.', ARRAY['linkedin'], 'ai', '{"seeded":true}'::jsonb, 'starter-social'),
			($1::uuid, $2::uuid, 'blog', 'draft', 'Kako pristupamo složenim izazovima', 'Početna struktura blog članka.', 'Dobar rezultat počinje razumijevanjem cilja, publike i činjenica koje odluku mogu potkrijepiti.', ARRAY['blog'], 'ai', '{"seeded":true}'::jsonb, 'starter-blog'),
			($1::uuid, $2::uuid, 'newsletter', 'draft', 'Prvi pregled projekta', 'Newsletter predložak za vlastitu publiku.', 'Kratki pregled najvažnijih ideja, projekata i sljedećih koraka.', ARRAY['newsletter'], 'ai', '{"seeded":true}'::jsonb, 'starter-newsletter')`, projectID, userID, organizationName)
	return err
}

const unlimitedFeaturesJSON = `{"aiAgents":true,"analytics":true,"api":true,"auditLog":true,"automations":true,"prioritySupport":true,"socialChannels":"all","whiteLabel":true}`

func unlimitedEntitlement() Entitlement {
	return Entitlement{
		PlanCode: "unlimited",
		Status:   "active",
		Features: map[string]any{
			"aiAgents": true, "analytics": true, "api": true, "auditLog": true,
			"automations": true, "prioritySupport": true, "socialChannels": "all",
			"whiteLabel": true,
		},
	}
}

func (r *Repository) Authenticate(ctx context.Context, email, password string) (User, error) {
	var user User
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, email, display_name, status, password_hash, last_login_at, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1) AND status = 'active'`, email).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Status, &user.PasswordHash,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return User{}, ErrNotFound
	}
	if _, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1::uuid`, user.ID); err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	user.LastLoginAt = &now
	return user, nil
}

func (r *Repository) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(ttl)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_sessions (user_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)`, userID, tokenHash[:], expiresAt)
	return token, expiresAt, err
}

func (r *Repository) ResolveSession(ctx context.Context, token string) (User, error) {
	tokenHash := sha256.Sum256([]byte(token))
	var user User
	err := r.pool.QueryRow(ctx, `
		UPDATE user_sessions AS session
		SET last_seen_at = now()
		FROM users
		WHERE session.token_hash = $1 AND session.expires_at > now()
		  AND users.id = session.user_id AND users.status = 'active'
		RETURNING users.id::text, users.email, users.display_name, users.status,
		          users.last_login_at, users.created_at, users.updated_at`, tokenHash[:]).Scan(
		&user.ID, &user.Email, &user.DisplayName, &user.Status,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

func (r *Repository) DeleteSession(ctx context.Context, token string) error {
	tokenHash := sha256.Sum256([]byte(token))
	_, err := r.pool.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, tokenHash[:])
	return err
}

func (r *Repository) ListProjectAccess(ctx context.Context, userID string) ([]ProjectAccess, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT project.id::text, project.name, project.slug, project.default_locale,
		       member.role, member.permissions,
		       entitlement.plan_code, entitlement.status, entitlement.seat_limit,
		       entitlement.monthly_publication_limit, entitlement.storage_limit_bytes,
		       entitlement.features, entitlement.renews_at
		FROM project_members AS member
		JOIN projects AS project ON project.id = member.project_id AND project.status = 'active'
		JOIN project_entitlements AS entitlement ON entitlement.project_id = project.id
		WHERE member.user_id = $1::uuid AND member.status = 'active'
		ORDER BY member.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accesses := make([]ProjectAccess, 0)
	for rows.Next() {
		var access ProjectAccess
		if err := rows.Scan(
			&access.ProjectID, &access.ProjectName, &access.ProjectSlug, &access.DefaultLocale,
			&access.Role, &access.Permissions,
			&access.Entitlement.PlanCode, &access.Entitlement.Status, &access.Entitlement.SeatLimit,
			&access.Entitlement.MonthlyPublicationLimit, &access.Entitlement.StorageLimitBytes,
			&access.Entitlement.Features, &access.Entitlement.RenewsAt,
		); err != nil {
			return nil, err
		}
		accesses = append(accesses, access)
	}
	return accesses, rows.Err()
}

func (r *Repository) ProjectRole(ctx context.Context, userID, projectID string) (string, map[string]any, error) {
	var role string
	var permissions map[string]any
	err := r.pool.QueryRow(ctx, `
		SELECT role, permissions
		FROM project_members
		WHERE user_id = $1::uuid AND project_id = $2::uuid AND status = 'active'`, userID, projectID).Scan(&role, &permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	return role, permissions, err
}

func postgresCode(err error) string {
	var pgError interface{ SQLState() string }
	if errors.As(err, &pgError) {
		return pgError.SQLState()
	}
	return ""
}
