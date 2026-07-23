package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/ivanorka/millena-ai/internal/config"
	"github.com/ivanorka/millena-ai/internal/database"
	"github.com/ivanorka/millena-ai/internal/httpapi"
	"github.com/ivanorka/millena-ai/internal/notification"
)

var (
	apiOnce     sync.Once
	api         http.Handler
	apiErr      error
	emailWorker *notification.Worker
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (*events.APIGatewayProxyResponse, error) {
	if err := initializeAPI(ctx); err != nil {
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusServiceUnavailable, Body: `{"error":{"code":"service_unavailable","message":"API initialization failed"}}`}, nil
	}

	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return &events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: `{"error":{"code":"invalid_body","message":"Invalid request body"}}`}, nil
		}
		body = decoded
	}

	path := event.Path
	const functionPrefix = "/.netlify/functions/api"
	if strings.HasPrefix(path, functionPrefix) {
		path = strings.TrimPrefix(path, functionPrefix)
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/api/") {
		path = "/api" + path
	}

	request, err := http.NewRequestWithContext(ctx, event.HTTPMethod, "https://millena.ai"+path, bytes.NewReader(body))
	if err != nil {
		return &events.APIGatewayProxyResponse{StatusCode: http.StatusBadRequest, Body: `{"error":{"code":"invalid_request","message":"Invalid request"}}`}, nil
	}
	for key, values := range event.MultiValueHeaders {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	for key, value := range event.Headers {
		if request.Header.Get(key) == "" {
			request.Header.Set(key, value)
		}
	}
	request.URL.RawQuery = encodeQuery(event.MultiValueQueryStringParameters, event.QueryStringParameters)

	recorder := httptest.NewRecorder()
	api.ServeHTTP(recorder, request)
	if emailWorker != nil {
		emailWorker.DeliverPending(ctx, 10)
	}
	response := recorder.Result()
	defer response.Body.Close()

	return &events.APIGatewayProxyResponse{
		StatusCode:        response.StatusCode,
		Headers:           flattenHeaders(response.Header),
		MultiValueHeaders: response.Header,
		Body:              recorder.Body.String(),
	}, nil
}

func initializeAPI(ctx context.Context) error {
	apiOnce.Do(func() {
		cfg, err := config.Load()
		if err != nil {
			apiErr = err
			return
		}
		pool, err := database.Open(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConnections)
		if err != nil {
			apiErr = err
			return
		}
		api = httpapi.NewRouter(httpapi.RouterOptions{
			Database: pool, AllowedOrigins: []string{"https://millena.ai"}, SessionTTL: cfg.SessionTTL,
			CookieSecure: true, AIProvider: cfg.AIProvider, OllamaBaseURL: cfg.OllamaBaseURL,
			OllamaModel: cfg.OllamaModel, AITimeout: cfg.AIRequestTimeout,
		})
		emailWorker = notification.NewWorker(notification.NewRepository(pool), notification.NewSMTPMailer(notification.SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.EmailFrom, FromName: cfg.EmailFromName, AppURL: cfg.AppBaseURL,
		}), 0)
	})
	if apiErr != nil {
		log.Printf("api initialization failed: %v", apiErr)
	}
	return apiErr
}

func encodeQuery(multi map[string][]string, single map[string]string) string {
	values := url.Values{}
	for key, entries := range multi {
		for _, entry := range entries {
			values.Add(key, entry)
		}
	}
	for key, value := range single {
		if _, exists := multi[key]; !exists {
			values.Add(key, value)
		}
	}
	return values.Encode()
}

func flattenHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}

func main() {
	lambda.Start(handler)
}
