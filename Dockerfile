FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/millena-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags="-s -w" github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.0

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/millena-api ./millena-api
COPY --from=build /go/bin/migrate ./migrate
COPY migrations ./migrations
COPY docker-entrypoint.sh ./docker-entrypoint.sh
COPY index.html login.html app.html site.css styles.css site.js script.js app-api.js ./public/
COPY assets ./public/assets
RUN chmod 0555 /app/docker-entrypoint.sh /app/migrate /app/millena-api

ENV APP_ENV=production \
    PORT=8080 \
    STATIC_DIR=/app/public

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/docker-entrypoint.sh"]
