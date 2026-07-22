package admin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ivanorka/millena-ai/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemHandler struct{ pool *pgxpool.Pool }

func NewSystemHandler(pool *pgxpool.Pool) *SystemHandler { return &SystemHandler{pool: pool} }

func (h *SystemHandler) Users(c *gin.Context) {
	rows, err := h.pool.Query(c, `SELECT id::text,email,display_name,status,system_role,created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		writeError(c, 500, "internal_error", "Users could not be loaded.")
		return
	}
	defer rows.Close()
	result := []gin.H{}
	for rows.Next() {
		var id, email, name, status, role string
		var created any
		if err := rows.Scan(&id, &email, &name, &status, &role, &created); err != nil {
			writeError(c, 500, "internal_error", "Users could not be loaded.")
			return
		}
		result = append(result, gin.H{"id": id, "email": email, "displayName": name, "status": status, "systemRole": role, "createdAt": created})
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
func (h *SystemHandler) UpdateUser(c *gin.Context) {
	var input struct {
		DisplayName string `json:"displayName"`
		Status      string `json:"status"`
		SystemRole  string `json:"systemRole"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.DisplayName) == "" {
		writeError(c, 422, "validation_error", "Name is required.")
		return
	}
	if input.Status != "active" && input.Status != "suspended" {
		writeError(c, 422, "validation_error", "Invalid user status.")
		return
	}
	if input.SystemRole != "member" && input.SystemRole != "super_admin" {
		writeError(c, 422, "validation_error", "Invalid system role.")
		return
	}
	var id, email, name, status, role string
	err := h.pool.QueryRow(c, `UPDATE users SET display_name=$2,status=$3,system_role=$4,updated_at=now() WHERE id=$1::uuid RETURNING id::text,email,display_name,status,system_role`, c.Param("userID"), strings.TrimSpace(input.DisplayName), input.Status, input.SystemRole).Scan(&id, &email, &name, &status, &role)
	if err != nil {
		writeError(c, 404, "not_found", "User was not found.")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"id": id, "email": email, "displayName": name, "status": status, "systemRole": role}})
}
func (h *SystemHandler) BulkUserStatus(c *gin.Context) {
	var input struct {
		UserIDs []string `json:"userIds"`
		Status  string   `json:"status"`
	}
	if c.ShouldBindJSON(&input) != nil || len(input.UserIDs) == 0 || (input.Status != "active" && input.Status != "suspended") {
		writeError(c, 422, "validation_error", "Users and a valid status are required.")
		return
	}
	actor, _ := auth.CurrentUser(c)
	for _, id := range input.UserIDs {
		if id == actor.ID && input.Status == "suspended" {
			writeError(c, 409, "self_deactivation_denied", "You cannot deactivate your own super-admin account.")
			return
		}
	}
	result, err := h.pool.Exec(c, `UPDATE users SET status=$2, updated_at=now() WHERE id = ANY($1::uuid[])`, input.UserIDs, input.Status)
	if err != nil {
		writeError(c, 422, "validation_error", "One or more user IDs are invalid.")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"updated": result.RowsAffected()}})
}
func (h *SystemHandler) Plans(c *gin.Context) {
	rows, err := h.pool.Query(c, `SELECT code,name,description,price_cents,currency,billing_interval,monthly_publication_limit,is_active FROM plan_catalog ORDER BY price_cents`)
	if err != nil {
		writeError(c, 500, "internal_error", "Plans could not be loaded.")
		return
	}
	defer rows.Close()
	result := []gin.H{}
	for rows.Next() {
		var code, name, description, currency, interval string
		var price int
		var limit *int
		var active bool
		if err := rows.Scan(&code, &name, &description, &price, &currency, &interval, &limit, &active); err != nil {
			writeError(c, 500, "internal_error", "Plans could not be loaded.")
			return
		}
		result = append(result, gin.H{"code": code, "name": name, "description": description, "priceCents": price, "currency": currency, "billingInterval": interval, "monthlyPublicationLimit": limit, "isActive": active})
	}
	c.JSON(200, gin.H{"data": result})
}
func (h *SystemHandler) UpdatePlan(c *gin.Context) {
	var input struct {
		Name                    string `json:"name"`
		Description             string `json:"description"`
		PriceCents              int    `json:"priceCents"`
		MonthlyPublicationLimit *int   `json:"monthlyPublicationLimit"`
		IsActive                bool   `json:"isActive"`
	}
	if c.ShouldBindJSON(&input) != nil || strings.TrimSpace(input.Name) == "" || input.PriceCents < 0 {
		writeError(c, 422, "validation_error", "Invalid plan values.")
		return
	}
	var code, name, description string
	var price int
	var limit *int
	var active bool
	err := h.pool.QueryRow(c, `UPDATE plan_catalog SET name=$2,description=$3,price_cents=$4,monthly_publication_limit=$5,is_active=$6,updated_at=now() WHERE code=$1 RETURNING code,name,description,price_cents,monthly_publication_limit,is_active`, c.Param("code"), strings.TrimSpace(input.Name), strings.TrimSpace(input.Description), input.PriceCents, input.MonthlyPublicationLimit, input.IsActive).Scan(&code, &name, &description, &price, &limit, &active)
	if err != nil {
		writeError(c, 404, "not_found", "Plan was not found.")
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"code": code, "name": name, "description": description, "priceCents": price, "monthlyPublicationLimit": limit, "isActive": active}})
}
