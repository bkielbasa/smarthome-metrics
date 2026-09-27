# smarthome-metrics

[![CI](https://github.com/bartlomiejklimczak/smarthome-metrics/actions/workflows/ci.yml/badge.svg)](https://github.com/bartlomiejklimczak/smarthome-metrics/actions/workflows/ci.yml)
[![Docker Image](https://img.shields.io/badge/docker-bartlomiejklimczak%2Fsmarthome--metrics-blue?logo=docker)](https://hub.docker.com/r/bartlomiejklimczak/smarthome-metrics)

A lightweight, high-performance Go microservice designed to ingest smart home and IoT telemetry metrics over HTTP and persist them reliably into PostgreSQL.

---

## Table of Contents

- [Overview & Architecture](#overview--architecture)
- [API Endpoints](#api-endpoints)
  - [POST /metric/{category}/{metric_name}](#post-metriccategorymetric_name)
  - [GET /healthz](#get-healthz)
- [Configuration](#configuration)
- [Local Development](#local-development)
  - [Prerequisites](#prerequisites)
  - [Running PostgreSQL](#running-postgresql)
  - [Running the Go Service](#running-the-go-service)
  - [Running with Docker](#running-with-docker)
- [Running Tests](#running-tests)
- [Kubernetes Deployment (Helm)](#kubernetes-deployment-helm)
  - [Prerequisites](#prerequisites-1)
  - [Installing the Chart](#installing-the-chart)
  - [Database Configuration Patterns](#database-configuration-patterns)
  - [Linting and Templating](#linting-and-templating)
- [CI/CD & Releases](#cicd--releases)
- [License](#license)

---

## Overview & Architecture

`smarthome-metrics` acts as a centralized telemetry ingestion point for IoT devices, microcontrollers (e.g. ESP8266/ESP32, Raspberry Pi), home automation engines (e.g. Home Assistant), and ambient sensors.

### Key Architectural Highlights

- **Standard Library HTTP Router:** Built on Go 1.22+ enhanced `net/http` routing with zero external routing dependencies.
- **Robust Database Connectivity:** Uses [`jackc/pgx/v5`](https://github.com/jackc/pgx) with connection pooling (`pgxpool`), health checks, and lifecycle management.
- **Auto-Migration on Startup:** Automatically provisions the `metrics` table and creates composite indexes (`category`, `metric_name`, `timestamp DESC`) on boot without external migration tools.
- **Graceful Shutdown:** Intercepts `SIGINT` and `SIGTERM` to safely drain active HTTP connections before closing database connection pools.
- **Minimal, Hardened Containers:** Uses a multi-stage Docker build producing an unprivileged Alpine container (< 25MB) running as non-root user `10001:10001` with a read-only root filesystem.

```
+------------------------+
| Smart Home / IoT Device|
+-----------+------------+
            |
            | HTTP POST (e.g., 21.5)
            v
+------------------------+
|   smarthome-metrics    |
|   (:8088 / Go 1.26)    |
+-----------+------------+
            |
            | pgxpool connection
            v
+------------------------+
|       PostgreSQL       |
|    (metrics table)     |
+------------------------+
```

---

## API Endpoints

### POST `/metric/{category}/{metric_name}`

Ingests a single floating-point metric value for the specified category and metric name.

#### URL Parameters

| Parameter | Type | Description | Validation Rule |
| :--- | :--- | :--- | :--- |
| `category` | `string` | Metric group or location (e.g., `living_room`, `hvac`, `power`) | `^[a-zA-Z0-9_\-\.]{1,128}$` |
| `metric_name` | `string` | Telemetry metric identifier (e.g., `temperature`, `humidity`, `watts`) | `^[a-zA-Z0-9_\-\.]{1,128}$` |

#### Request Body

- **Format:** Plain text representing a 64-bit float (e.g., `21.5`, `-4.2`, `1013.25`).
- **Maximum Size:** 64 KB.

#### Responses

- **`200 OK`**: Metric successfully persisted.
  ```json
  {
    "status": "ok"
  }
  ```

- **`400 Bad Request`**: Validation failed (invalid identifier slug, empty body, or non-float payload).
  ```json
  {
    "error": "invalid category or metric_name; must match ^[a-zA-Z0-9_\\-\\.]{1,128}$"
  }
  ```

- **`500 Internal Server Error`**: Database error during persistence.
  ```json
  {
    "error": "failed to persist metric"
  }
  ```

#### Example `curl` Requests

```bash
# Ingest ambient temperature
curl -X POST http://localhost:8088/metric/living_room/temperature \
  -H "Content-Type: text/plain" \
  -d "21.5"

# Ingest relative humidity
curl -X POST http://localhost:8088/metric/living_room/humidity \
  -H "Content-Type: text/plain" \
  -d "48.2"

# Ingest power usage in watts
curl -X POST http://localhost:8088/metric/home/power_consumption \
  -H "Content-Type: text/plain" \
  -d "1250.75"
```

---

### GET `/healthz`

Performs an active database ping to verify service and persistence readiness.

#### Responses

- **`200 OK`**: Service and database are healthy.
  ```json
  {
    "status": "healthy",
    "database": "connected"
  }
  ```

- **`503 Service Unavailable`**: Database is unreachable or query timed out.
  ```json
  {
    "status": "unhealthy",
    "error": "failed to connect to server..."
  }
  ```

#### Example `curl` Request

```bash
curl -i http://localhost:8088/healthz
```

---

## Configuration

All configuration is supplied via environment variables.

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `8088` | TCP port the HTTP server listens on |
| `DATABASE_URL` | *(empty)* | Complete PostgreSQL connection string. When set, this overrides individual `DB_*` variables |
| `DB_HOST` | `localhost` | PostgreSQL hostname or IP |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL username |
| `DB_PASSWORD` | *(empty)* | PostgreSQL password |
| `DB_NAME` | `metrics` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | SSL mode (`disable`, `require`, `verify-ca`, `verify-full`) |
| `SHUTDOWN_TIMEOUT` | `5s` | Duration allowed for active HTTP requests to complete during shutdown (e.g. `5s`, `10s`) |

---

## Local Development

### Prerequisites

- [Go](https://golang.org/) 1.26 or higher
- [Docker](https://www.docker.com/) (optional, for local PostgreSQL or container run)

### Running PostgreSQL

Start an ephemeral PostgreSQL instance with Docker:

```bash
docker run -d --name metrics-db \
  -e POSTGRES_DB=metrics \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine
```

### Running the Go Service

Set connection variables and start the server:

```bash
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=metrics

go run ./cmd/server
```

Or using `DATABASE_URL`:

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/metrics?sslmode=disable"
go run ./cmd/server
```

### Running with Docker

Build and run the container locally:

```bash
# Build container image
docker build -t smarthome-metrics .

# Run container connected to host database
docker run --rm -p 8088:8088 \
  -e DB_HOST=host.docker.internal \
  -e DB_PORT=5432 \
  -e DB_USER=postgres \
  -e DB_PASSWORD=postgres \
  -e DB_NAME=metrics \
  smarthome-metrics
```

---

## Running Tests

Run the full unit and integration test suite with data race detection enabled:

```bash
go test -v -race ./...
```

Run test suite with coverage report:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

---

## Kubernetes Deployment (Helm)

The repository provides a production-grade Helm chart under [`charts/smarthome-metrics`](charts/smarthome-metrics).

### Prerequisites

- Kubernetes cluster 1.26+
- [Helm](https://helm.sh/) v3+
- An existing or external PostgreSQL database

### Installing the Chart

Add or specify your deployment parameters:

```bash
helm install smarthome-metrics ./charts/smarthome-metrics \
  --set database.host="postgres.database.svc.cluster.local" \
  --set database.password="mysecretpassword"
```

### Database Configuration Patterns

#### 1. Using a Pre-existing Secret for the Database Password

To avoid plaintext passwords in Helm values, reference an existing Kubernetes Secret:

```bash
# Create secret manually or via SealedSecrets / External Secrets Operator
kubectl create secret generic smarthome-db-credentials \
  --from-literal=postgresql-password='super-secret-password'

# Install chart referencing the secret
helm install smarthome-metrics ./charts/smarthome-metrics \
  --set database.host="postgres.database.svc.cluster.local" \
  --set database.existingSecret="smarthome-db-credentials" \
  --set database.existingSecretPasswordKey="postgresql-password"
```

#### 2. Using a Pre-existing Secret Containing Full `DATABASE_URL`

If you manage full connection URIs (e.g., from CloudNative-PG or external managed database):

```bash
kubectl create secret generic smarthome-db-url \
  --from-literal=DATABASE_URL='postgres://user:pass@postgres.database.svc.cluster.local:5432/metrics?sslmode=require'

helm install smarthome-metrics ./charts/smarthome-metrics \
  --set database.existingSecret="smarthome-db-url" \
  --set database.urlSecretKey="DATABASE_URL"
```

### Linting and Templating

Validate and inspect the chart locally:

```bash
# Lint the chart
helm lint charts/smarthome-metrics

# Render templates with default values
helm template test charts/smarthome-metrics

# Render templates with external secret configuration
helm template test charts/smarthome-metrics \
  --set database.existingSecret=smarthome-db-credentials
```

---

## CI/CD & Releases

This project uses **GitHub Actions** for automated testing and releases:

### Continuous Integration (`ci.yml`)
- Triggered on every `push` and `pull_request` targeting the `main` branch.
- Runs the test suite with race detection (`go test -v -race ./...`).
- Verifies server binary compilation (`go build -v ./cmd/server`).
- Tests container image builds via Docker Buildx.

### Automated Releases (`release.yml`)
- Triggered whenever a Git tag matching `v*` (e.g. `v1.0.0`) is pushed:
  ```bash
  git tag v1.0.0
  git push origin v1.0.0
  ```
- Steps executed:
  1. Runs all unit tests.
  2. Sets up QEMU and Docker Buildx.
  3. Builds multi-architecture container images for **`linux/amd64`** and **`linux/arm64`**.
  4. Pushes tagged images to Docker Hub under [`bartlomiejklimczak/smarthome-metrics`](https://hub.docker.com/r/bartlomiejklimczak/smarthome-metrics) (`v1.0.0`, `1.0`, `latest`).
  5. Generates a new **GitHub Release** with automated release notes.

---

## License

This project is licensed under the MIT License.
