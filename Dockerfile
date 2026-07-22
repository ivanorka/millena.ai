FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/millena-api ./cmd/api

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=build /out/millena-api ./millena-api
COPY index.html login.html app.html site.css styles.css site.js script.js app-api.js ./public/
COPY assets ./public/assets

ENV APP_ENV=production \
    PORT=8080 \
    STATIC_DIR=/app/public

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/millena-api"]
