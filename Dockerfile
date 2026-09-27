# Stage 1: Build binary
FROM golang:1.26-alpine AS builder

WORKDIR /build

RUN apk add --no-cache ca-certificates tzdata git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/smarthome-metrics \
    ./cmd/server

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 appgroup \
    && adduser -u 10001 -G appgroup -s /sbin/nologin -D appuser

WORKDIR /app

COPY --from=builder /app/smarthome-metrics /app/smarthome-metrics

USER 10001:10001

EXPOSE 8088

ENTRYPOINT ["/app/smarthome-metrics"]
