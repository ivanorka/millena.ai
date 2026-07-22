package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const entitlementLookupTimeout = 2 * time.Second

// requireProjectFeature verifies the current entitlement on every request so
// plan changes take effect immediately and cannot be bypassed by a stale UI.
// Project membership is deliberately checked by the role middleware first.
func requireProjectFeature(pool *pgxpool.Pool, feature string) gin.HandlerFunc {
	feature = strings.TrimSpace(feature)
	return func(c *gin.Context) {
		if !projectHasFeature(c, pool, feature) {
			return
		}

		c.Next()
	}
}

func projectHasFeature(c *gin.Context, pool *pgxpool.Pool, feature string) bool {
	if pool == nil {
		abortFeatureRequest(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
		return false
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), entitlementLookupTimeout)
	defer cancel()

	var status string
	var features map[string]any
	err := pool.QueryRow(ctx, `
		SELECT status, features
		FROM project_entitlements
		WHERE project_id = $1::uuid`, c.Param("projectID")).Scan(&status, &features)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		abortFeatureRequest(c, http.StatusForbidden, "entitlement_missing", "The project does not have an active plan.")
		return false
	case err != nil:
		var pgError *pgconn.PgError
		if errors.As(err, &pgError) && pgError.Code == "22P02" {
			abortFeatureRequest(c, http.StatusBadRequest, "invalid_id", "Project ID must be a UUID.")
			return false
		}
		abortFeatureRequest(c, http.StatusInternalServerError, "entitlement_lookup_failed", "Project plan could not be verified.")
		return false
	}

	if status != "active" && status != "trial" {
		abortFeatureRequest(c, http.StatusForbidden, "entitlement_inactive", "The project plan is not active.")
		return false
	}
	if !projectFeatureEnabled(features, feature) {
		abortFeatureRequest(c, http.StatusForbidden, "feature_not_available", "This feature is not included in the current plan.")
		return false
	}
	return true
}

func projectFeatureEnabled(features map[string]any, feature string) bool {
	enabled, ok := features[feature].(bool)
	return ok && enabled
}

func abortFeatureRequest(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
