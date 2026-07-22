package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanorka/millena-ai/internal/actions"
	"github.com/ivanorka/millena-ai/internal/admin"
	"github.com/ivanorka/millena-ai/internal/assets"
	"github.com/ivanorka/millena-ai/internal/assistant"
	"github.com/ivanorka/millena-ai/internal/audience"
	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/ivanorka/millena-ai/internal/calendar"
	"github.com/ivanorka/millena-ai/internal/content"
	"github.com/ivanorka/millena-ai/internal/projects"
	"github.com/ivanorka/millena-ai/internal/social"
	"github.com/ivanorka/millena-ai/internal/workspace"
)

type RouterOptions struct {
	Database       *pgxpool.Pool
	StaticDir      string
	AllowedOrigins []string
	SessionTTL     time.Duration
	CookieSecure   bool
	AIProvider     string
	OllamaBaseURL  string
	OllamaModel    string
	AITimeout      time.Duration
}

func NewRouter(options RouterOptions) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), securityHeaders(), cors(options.AllowedOrigins))
	_ = router.SetTrustedProxies(nil)

	api := router.Group("/api/v1")
	api.GET("/health", liveness)
	api.GET("/ready", readiness(options.Database))

	authHandler := auth.NewHandler(auth.NewRepository(options.Database), options.SessionTTL, options.CookieSecure)
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	secured := api.Group("")
	secured.Use(authHandler.RequireSession())
	secured.GET("/auth/me", authHandler.Me)
	secured.POST("/auth/logout", authHandler.Logout)

	allProjectRoles := authHandler.RequireProjectRoles("owner", "lead", "editor", "contributor", "viewer")
	writeProjectRoles := authHandler.RequireProjectRoles("owner", "lead", "editor", "contributor")
	manageProjectRoles := authHandler.RequireProjectRoles("owner", "lead")
	publishProjectRoles := authHandler.RequireProjectRoles("owner", "lead", "editor")
	ownerProjectRole := authHandler.RequireProjectRoles("owner")
	requireAIAgents := requireProjectFeature(options.Database, "aiAgents")
	requireAuditLog := requireProjectFeature(options.Database, "auditLog")
	requireAutomations := requireProjectFeature(options.Database, "automations")

	projectHandler := projects.NewHandler(projects.NewRepository(options.Database))
	secured.GET("/projects", projectHandler.List)
	secured.POST("/projects", projectHandler.Create)
	secured.GET("/projects/:projectID", allProjectRoles, projectHandler.Get)
	secured.DELETE("/projects/:projectID", ownerProjectRole, projectHandler.Delete)
	secured.GET("/projects/:projectID/state", allProjectRoles, projectHandler.GetAppState)
	secured.PUT("/projects/:projectID/state", writeProjectRoles, projectHandler.SaveAppState)

	assetHandler := assets.NewHandler(assets.NewRepository(options.Database))
	secured.GET("/projects/:projectID/assets", allProjectRoles, assetHandler.List)
	secured.POST("/projects/:projectID/assets", writeProjectRoles, assetHandler.Upload)
	secured.GET("/projects/:projectID/assets/:assetID", allProjectRoles, assetHandler.Get)
	secured.PUT("/projects/:projectID/assets/:assetID", publishProjectRoles, assetHandler.Update)
	secured.GET("/projects/:projectID/assets/:assetID/download", allProjectRoles, assetHandler.Download)
	secured.DELETE("/projects/:projectID/assets/:assetID", publishProjectRoles, assetHandler.Delete)

	socialHandler := social.NewHandler(social.NewRepository(options.Database))
	secured.GET("/projects/:projectID/social/connections", allProjectRoles, socialHandler.ListConnections)
	secured.POST("/projects/:projectID/social/connections", manageProjectRoles, socialHandler.Connect)
	secured.POST("/projects/:projectID/social/connections/:connectionID/test", manageProjectRoles, socialHandler.TestConnection)
	secured.DELETE("/projects/:projectID/social/connections/:connectionID", manageProjectRoles, socialHandler.Disconnect)
	secured.GET("/projects/:projectID/social/posts", allProjectRoles, socialHandler.ListPosts)
	secured.POST("/projects/:projectID/social/posts", publishProjectRoles, socialHandler.CreatePost)

	calendarHandler := calendar.NewHandler(calendar.NewRepository(options.Database))
	secured.GET("/projects/:projectID/calendar", allProjectRoles, calendarHandler.List)
	secured.GET("/projects/:projectID/calendar/items/:itemID", allProjectRoles, calendarHandler.Get)
	secured.POST("/projects/:projectID/calendar/items", publishProjectRoles, calendarHandler.Create)
	secured.PUT("/projects/:projectID/calendar/items/:itemID", publishProjectRoles, calendarHandler.Update)
	secured.DELETE("/projects/:projectID/calendar/items/:itemID", manageProjectRoles, calendarHandler.Delete)

	aiService := content.NewAIService(content.AIOptions{
		Provider: options.AIProvider, OllamaBaseURL: options.OllamaBaseURL,
		OllamaModel: options.OllamaModel, Timeout: options.AITimeout,
	})
	contentHandler := content.NewHandler(content.NewRepository(options.Database), aiService)
	secured.GET("/projects/:projectID/content", allProjectRoles, contentHandler.List)
	secured.GET("/projects/:projectID/content/items/:itemID", allProjectRoles, contentHandler.Get)
	secured.POST("/projects/:projectID/content/items", publishProjectRoles, contentHandler.Create)
	secured.PUT("/projects/:projectID/content/items/:itemID", publishProjectRoles, contentHandler.Update)
	secured.POST("/projects/:projectID/content/items/:itemID/review", publishProjectRoles, contentHandler.ApproveReview)
	secured.DELETE("/projects/:projectID/content/items/:itemID", manageProjectRoles, contentHandler.Delete)
	secured.GET("/projects/:projectID/content/items/:itemID/variants", allProjectRoles, contentHandler.ListVariants)
	secured.PUT("/projects/:projectID/content/items/:itemID/variants", publishProjectRoles, contentHandler.SaveVariant)
	secured.DELETE("/projects/:projectID/content/items/:itemID/variants/:variantID", manageProjectRoles, contentHandler.DeleteVariant)
	secured.GET("/projects/:projectID/strategy", allProjectRoles, contentHandler.GetStrategy)
	secured.PUT("/projects/:projectID/strategy", publishProjectRoles, contentHandler.SaveStrategy)
	secured.POST("/projects/:projectID/strategy/file", publishProjectRoles, contentHandler.UploadStrategy)
	secured.GET("/projects/:projectID/content/ai/status", allProjectRoles, contentHandler.AIStatus)
	secured.POST("/projects/:projectID/content/ai", publishProjectRoles, requireAIAgents, contentHandler.RunAI)

	workspaceHandler := workspace.NewHandler(workspace.NewRepository(options.Database))
	secured.GET("/projects/:projectID/profile", allProjectRoles, workspaceHandler.GetProfile)
	secured.PUT("/projects/:projectID/profile", manageProjectRoles, workspaceHandler.SaveProfile)
	secured.GET("/projects/:projectID/dashboard", allProjectRoles, workspaceHandler.GetDashboard)
	secured.GET("/projects/:projectID/automations", allProjectRoles, requireAutomations, workspaceHandler.ListAutomationRules)
	secured.POST("/projects/:projectID/automations", manageProjectRoles, requireAutomations, workspaceHandler.CreateAutomationRule)
	secured.PUT("/projects/:projectID/automations/:ruleID", manageProjectRoles, requireAutomations, workspaceHandler.UpdateAutomationRule)
	secured.DELETE("/projects/:projectID/automations/:ruleID", manageProjectRoles, requireAutomations, workspaceHandler.DeleteAutomationRule)
	secured.POST("/projects/:projectID/automations/:ruleID/run", publishProjectRoles, requireAutomations, workspaceHandler.RunAutomationRule)
	secured.GET("/projects/:projectID/channel-connections", allProjectRoles, workspaceHandler.ListChannelConnections)
	secured.POST("/projects/:projectID/channel-connections", manageProjectRoles, workspaceHandler.CreateChannelConnection)
	secured.PUT("/projects/:projectID/channel-connections/:connectionID", manageProjectRoles, workspaceHandler.UpdateChannelConnection)
	secured.DELETE("/projects/:projectID/channel-connections/:connectionID", manageProjectRoles, workspaceHandler.DeleteChannelConnection)
	secured.POST("/projects/:projectID/channel-connections/:connectionID/test", manageProjectRoles, workspaceHandler.TestChannelConnection)
	secured.GET("/projects/:projectID/service-requests", manageProjectRoles, workspaceHandler.ListServiceRequests)
	secured.POST("/projects/:projectID/service-requests", manageProjectRoles, workspaceHandler.CreateServiceRequest)
	secured.PUT("/projects/:projectID/service-requests/:requestID", manageProjectRoles, workspaceHandler.UpdateServiceRequest)
	secured.GET("/projects/:projectID/personas", allProjectRoles, workspaceHandler.ListProjectPersonas)
	secured.POST("/projects/:projectID/personas", publishProjectRoles, workspaceHandler.CreateProjectPersona)
	secured.PUT("/projects/:projectID/personas/:personaID", publishProjectRoles, workspaceHandler.UpdateProjectPersona)
	secured.DELETE("/projects/:projectID/personas/:personaID", manageProjectRoles, workspaceHandler.DeleteProjectPersona)
	secured.GET("/projects/:projectID/newsletter/deliveries", allProjectRoles, workspaceHandler.ListNewsletterDeliveries)
	secured.POST("/projects/:projectID/newsletter/deliveries", publishProjectRoles, workspaceHandler.CreateNewsletterDelivery)

	audienceHandler := audience.NewHandler(audience.NewRepository(options.Database))
	secured.GET("/projects/:projectID/audience/lists", publishProjectRoles, audienceHandler.Lists)
	secured.POST("/projects/:projectID/audience/lists", publishProjectRoles, audienceHandler.CreateList)
	secured.PUT("/projects/:projectID/audience/lists/:listID", manageProjectRoles, audienceHandler.UpdateList)
	secured.DELETE("/projects/:projectID/audience/lists/:listID", manageProjectRoles, audienceHandler.DeleteList)
	secured.GET("/projects/:projectID/audience/contacts", publishProjectRoles, audienceHandler.Contacts)
	secured.GET("/projects/:projectID/audience/contacts/:contactID", publishProjectRoles, audienceHandler.GetContact)
	secured.POST("/projects/:projectID/audience/contacts", publishProjectRoles, audienceHandler.CreateContact)
	secured.PUT("/projects/:projectID/audience/contacts/:contactID", publishProjectRoles, audienceHandler.UpdateContact)
	secured.DELETE("/projects/:projectID/audience/contacts/:contactID", manageProjectRoles, audienceHandler.DeleteContact)
	secured.POST("/projects/:projectID/audience/import/csv", publishProjectRoles, audienceHandler.ImportCSV)

	assistantRepository := assistant.NewRepository(options.Database)
	assistantHandler := assistant.NewHandler(assistantRepository, assistant.NewService(assistantRepository, aiService))
	secured.GET("/projects/:projectID/assistant/status", allProjectRoles, requireAIAgents, assistantHandler.Status)
	secured.GET("/projects/:projectID/assistant/threads", allProjectRoles, requireAIAgents, assistantHandler.Threads)
	secured.POST("/projects/:projectID/assistant/threads", publishProjectRoles, requireAIAgents, assistantHandler.CreateThread)
	secured.GET("/projects/:projectID/assistant/threads/:threadID/messages", allProjectRoles, requireAIAgents, assistantHandler.Messages)
	secured.POST("/projects/:projectID/assistant/threads/:threadID/messages", publishProjectRoles, requireAIAgents, assistantHandler.Send)

	adminHandler := admin.NewHandler(admin.NewRepository(options.Database))
	secured.GET("/projects/:projectID/team", manageProjectRoles, adminHandler.ListTeam)
	secured.POST("/projects/:projectID/team", ownerProjectRole, adminHandler.CreateMember)
	secured.PUT("/projects/:projectID/team/:memberID", ownerProjectRole, adminHandler.UpdateMember)
	secured.DELETE("/projects/:projectID/team/:memberID", ownerProjectRole, adminHandler.DeleteMember)
	secured.GET("/projects/:projectID/plans", allProjectRoles, adminHandler.ListPlans)
	secured.POST("/projects/:projectID/plans", ownerProjectRole, adminHandler.CreatePlan)
	secured.GET("/projects/:projectID/entitlement", allProjectRoles, adminHandler.GetEntitlement)
	secured.PUT("/projects/:projectID/entitlement", ownerProjectRole, adminHandler.UpdateEntitlement)

	actionHandler := actions.NewHandler(actions.NewRepository(options.Database))
	secured.POST("/projects/:projectID/actions", allProjectRoles, actionHandler.Record)
	secured.GET("/projects/:projectID/actions", manageProjectRoles, requireAuditLog, actionHandler.List)

	registerStaticRoutes(router, options.StaticDir)
	return router
}

func liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"service": "millena-api",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func readiness(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if pool == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "database": "unavailable"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready", "database": "unavailable"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ready", "database": "available"})
	}
}

func registerStaticRoutes(router *gin.Engine, staticDir string) {
	if staticDir == "" {
		return
	}

	router.Static("/assets", filepath.Join(staticDir, "assets"))
	router.StaticFile("/", filepath.Join(staticDir, "index.html"))
	for _, file := range []string{"index.html", "login.html", "app.html", "site.css", "styles.css", "site.js", "script.js", "app-api.js"} {
		router.StaticFile("/"+file, filepath.Join(staticDir, file))
	}
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

func cors(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowed[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
