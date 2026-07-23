package billing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler creates hosted Stripe Checkout sessions. Card details never reach
// Millena; Stripe hosts the payment form and remains the PCI boundary.
type Handler struct {
	pool            *pgxpool.Pool
	stripeSecretKey string
	appBaseURL      string
	httpClient      *http.Client
}

func NewHandler(pool *pgxpool.Pool, stripeSecretKey, appBaseURL string) *Handler {
	return &Handler{pool: pool, stripeSecretKey: strings.TrimSpace(stripeSecretKey), appBaseURL: strings.TrimRight(appBaseURL, "/"), httpClient: &http.Client{Timeout: 15 * time.Second}}
}

func (h *Handler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"provider": "stripe", "checkoutEnabled": h.stripeSecretKey != ""}})
}

type checkoutInput struct {
	PlanCode string `json:"planCode"`
}

func (h *Handler) CreateCheckoutSession(c *gin.Context) {
	if h.pool == nil {
		billingError(c, http.StatusServiceUnavailable, "database_unavailable", "Plans are unavailable.")
		return
	}
	if h.stripeSecretKey == "" {
		billingError(c, http.StatusServiceUnavailable, "stripe_not_configured", "Stripe test key is not configured yet.")
		return
	}
	var input checkoutInput
	if err := c.ShouldBindJSON(&input); err != nil {
		billingError(c, http.StatusUnprocessableEntity, "validation_error", "Choose a plan before continuing to payment.")
		return
	}
	requestedCode := strings.ToLower(strings.TrimSpace(input.PlanCode))
	catalogCode := requestedCode
	if requestedCode == "enterprise" {
		catalogCode = "unlimited"
	}
	if catalogCode != "starter" && catalogCode != "optimum" && catalogCode != "unlimited" {
		billingError(c, http.StatusUnprocessableEntity, "invalid_plan", "This plan cannot be purchased online.")
		return
	}
	var name, currency string
	var priceCents int
	err := h.pool.QueryRow(c, `SELECT name, currency, price_cents FROM plan_catalog WHERE code=$1 AND is_system AND is_active`, catalogCode).Scan(&name, &currency, &priceCents)
	if err != nil {
		billingError(c, http.StatusNotFound, "plan_not_found", "The selected plan is unavailable.")
		return
	}
	if priceCents <= 0 {
		billingError(c, http.StatusUnprocessableEntity, "contact_sales", "Contact us to activate this plan.")
		return
	}
	values := url.Values{}
	values.Set("mode", "subscription")
	values.Set("success_url", h.appBaseURL+"/login.html?checkout=success&session_id={CHECKOUT_SESSION_ID}")
	values.Set("cancel_url", h.appBaseURL+"/login.html?checkout=cancelled")
	values.Set("metadata[plan_code]", requestedCode)
	values.Set("line_items[0][price_data][currency]", strings.ToLower(currency))
	values.Set("line_items[0][price_data][unit_amount]", fmt.Sprintf("%d", priceCents))
	values.Set("line_items[0][price_data][recurring][interval]", "month")
	values.Set("line_items[0][price_data][product_data][name]", "Millena AI "+name)
	values.Set("line_items[0][quantity]", "1")
	req, err := http.NewRequestWithContext(c, http.MethodPost, "https://api.stripe.com/v1/checkout/sessions", strings.NewReader(values.Encode()))
	if err != nil {
		billingError(c, 500, "checkout_request_failed", "Payment session could not be prepared.")
		return
	}
	req.SetBasicAuth(h.stripeSecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := h.httpClient.Do(req)
	if err != nil {
		billingError(c, 502, "stripe_unreachable", "Stripe could not be reached. Please try again.")
		return
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		billingError(c, 502, "stripe_checkout_failed", "Stripe could not create a payment session.")
		return
	}
	var stripeResponse struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &stripeResponse); err != nil || stripeResponse.URL == "" {
		billingError(c, 502, "stripe_checkout_invalid", "Stripe returned an invalid payment session.")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"sessionId": stripeResponse.ID, "url": stripeResponse.URL}})
}

func billingError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
