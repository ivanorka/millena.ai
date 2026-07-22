package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

const userContextKey = "millena.auth.user"
const projectRoleContextKey = "millena.auth.projectRole"
const projectPermissionsContextKey = "millena.auth.projectPermissions"

func (h *Handler) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.repository == nil {
			writeError(c, http.StatusServiceUnavailable, "database_unavailable", "Database is not configured.")
			c.Abort()
			return
		}
		token, err := c.Cookie(sessionCookieName)
		if err != nil || token == "" {
			writeError(c, http.StatusUnauthorized, "authentication_required", "Sign-in is required.")
			c.Abort()
			return
		}
		user, err := h.repository.ResolveSession(c.Request.Context(), token)
		if errors.Is(err, ErrNotFound) {
			h.clearSessionCookie(c)
			writeError(c, http.StatusUnauthorized, "session_expired", "Your session has expired. Please sign in again.")
			c.Abort()
			return
		}
		if err != nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "Session could not be validated.")
			c.Abort()
			return
		}
		c.Set(userContextKey, user)
		c.Next()
	}
}

func (h *Handler) RequireProjectRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		user, ok := CurrentUser(c)
		if !ok {
			writeError(c, http.StatusUnauthorized, "authentication_required", "Sign-in is required.")
			c.Abort()
			return
		}
		role, permissions, err := h.repository.ProjectRole(c.Request.Context(), user.ID, c.Param("projectID"))
		if errors.Is(err, ErrNotFound) {
			writeError(c, http.StatusForbidden, "project_access_denied", "You do not have access to this project.")
			c.Abort()
			return
		}
		if err != nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "Project permissions could not be validated.")
			c.Abort()
			return
		}
		if _, ok := allowed[role]; !ok {
			writeError(c, http.StatusForbidden, "insufficient_permission", "Your project role does not allow this action.")
			c.Abort()
			return
		}
		c.Set(projectRoleContextKey, role)
		c.Set(projectPermissionsContextKey, permissions)
		c.Next()
	}
}

func CurrentUser(c *gin.Context) (User, bool) {
	value, ok := c.Get(userContextKey)
	if !ok {
		return User{}, false
	}
	user, ok := value.(User)
	return user, ok
}

func UserID(c *gin.Context) string {
	user, _ := CurrentUser(c)
	return user.ID
}

func ProjectRole(c *gin.Context) string {
	return c.GetString(projectRoleContextKey)
}
