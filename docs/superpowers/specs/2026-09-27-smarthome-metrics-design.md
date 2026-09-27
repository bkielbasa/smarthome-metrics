# Design Specification: SmartHome Metrics Collector & Deployment

## 1. Overview
The **SmartHome Metrics Collector** (`smarthome-metrics`) is a lightweight, high-performance Go web service designed to ingest time-series sensor metrics via HTTP POST requests and persist them into a PostgreSQL database.

This project encompasses:
1. The **Go microservice** providing HTTP metric ingestion and health checks.
2. **PostgreSQL schema** management with automatic migration.
3. **Multi-stage Dockerfile** for minimal footprint and multi-architecture compatibility (`amd64`, `arm64`).
4. **GitHub Actions CI/CD pipeline** pushing images to Docker Hub (`bartlomiejklimczak/smarthome-metrics`) and creating automated GitHub Releases on git tags.
5. **Helm Chart** (`charts/smarthome-metrics`) configured for Kubernetes deployment with external PostgreSQL connectivity.

---

## 2. API Contract & Endpoints

### 2.1 Metric Ingestion
* **Endpoint:** `POST /metric/{category}/{metric_name}`
* **Path Parameters:**
  * `category` (`string`, required): High-level grouping (e.g., `main_meter`, `living_room`). Validated to match regex `^[a-zA-Z0-9_\-\.]{1,128}$`.
  * `metric_name` (`string`, required): Specific metric identifier (e.g., `delivery_current`, `temperature`). Validated to match regex `^[a-zA-Z0-9_\-\.]{1,128}$`.
* **Request Body:**
  * Raw float formatted as text/plain or string, e.g. `230.5` or `14.2\n`.
  * Max body size: 64 KB (enforced via `http.MaxBytesReader`).
  * Body is trimmed of surrounding whitespace and parsed using `strconv.ParseFloat(rawBody, 64)`.
* **Response Codes:**
  * `200 OK`: Metric successfully persisted. Body: `{"status":"ok"}`.
  * `400 Bad Request`: Missing/invalid path parameters or invalid float in body. Body: `{"error":"<detail>"}`.
  * `500 Internal Server Error`: Database insertion error. Body: `{"error":"internal server error"}`.

### 2.2 Health Check & Probes
* **Endpoint:** `GET /healthz`
* **Purpose:** Kubernetes liveness and readiness probe.
* **Behavior:** Pings the PostgreSQL database connection pool (`db.Ping(ctx)`).
* **Response Codes:**
  * `200 OK`: Database connected and responsive. Body: `{"status":"healthy","database":"connected"}`.
  * `503 Service Unavailable`: Database ping fails or context times out (2s timeout). Body: `{"status":"unhealthy","error":"<detail>"}`.

---

## 3. Database Architecture & Schema

### 3.1 PostgreSQL Driver & Connection Pool
* Uses `jackc/pgx/v5` with connection pool `pgxpool.Pool`.
* Configurable connection pool settings (max connections: 25, min connections: 2, max conn lifetime: 1h, max idle time: 30m).

### 3.2 Schema Definition & Startup Migration
The service executes idempotent schema initialization on startup:
```sql
CREATE TABLE IF NOT EXISTS metrics (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    category VARCHAR(128) NOT NULL,
    metric_name VARCHAR(128) NOT NULL,
    value DOUBLE PRECISION NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_metrics_cat_metric_time 
ON metrics (category, metric_name, timestamp DESC);
```

### 3.3 Insertion Query
```sql
INSERT INTO metrics (timestamp, category, metric_name, value)
VALUES (NOW(), $1, $2, $3);
```

---

## 4. Application Architecture & Project Structure

### 4.1 Go Project Structure
```
smarthome-metrics/
├── cmd/
│   └── server/
│       └── main.go              # Application entrypoint & signal handling
├── internal/
│   ├── config/
│   │   ├── config.go            # Environment variable parsing & defaults
│   │   └── config_test.go
│   ├── db/
│   │   ├── db.go                # DB connection pool & migrations
│   │   └── store.go             # MetricStore interface & PgxStore implementation
│   └── server/
│       ├── handlers.go          # HTTP handler functions (POST metric, GET healthz)
│       ├── routes.go            # ServeMux setup & routing
│       └── server_test.go       # Unit & integration tests for HTTP layer
├── charts/
│   └── smarthome-metrics/       # Kubernetes Helm Chart
├── .github/
│   └── workflows/
│       ├── ci.yml               # CI test & build workflow on PR/push
│       └── release.yml          # Tag-driven Docker push & GitHub Release workflow
├── Dockerfile                   # Multi-stage production container
├── .dockerignore
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

### 4.2 Configuration Parameters
Configured via environment variables:
| Variable | Default | Description |
|---|---|---|
| `PORT` | `8088` | HTTP listening port |
| `DATABASE_URL` | `""` | Full PostgreSQL connection URI. If provided, overrides discrete DB params. |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL username |
| `DB_PASSWORD` | `""` | PostgreSQL password |
| `DB_NAME` | `metrics` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode (`disable`, `require`, `verify-full`) |
| `SHUTDOWN_TIMEOUT` | `5s` | Grace period for server drain during termination |

### 4.3 Graceful Shutdown
Listens for `os.Interrupt` and `syscall.SIGTERM`. On capture:
1. Stops accepting new HTTP connections via `httpServer.Shutdown(ctx)` with a 5s context.
2. Closes database pool `pgxpool.Close()`.
3. Exits with code 0.

---

## 5. Containerization (Dockerfile)

### 5.1 Multi-Stage Build
* **Stage 1: Build (`golang:1.24-alpine`)**
  * Installs `git`, `ca-certificates`, `tzdata`.
  * Copies `go.mod` and `go.sum`, runs `go mod download`.
  * Copies source code.
  * Compiles: `CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app/smarthome-metrics ./cmd/server`.
* **Stage 2: Runtime (`alpine:3.21`)**
  * Creates dedicated unprivileged group and user: `addgroup -g 10001 appgroup && adduser -u 10001 -G appgroup -s /sbin/nologin -D appuser`.
  * Copies `/etc/ssl/certs/ca-certificates.crt` and `/usr/share/zoneinfo`.
  * Copies `/app/smarthome-metrics`.
  * Sets `USER 10001:10001`.
  * Exposes port `8088`.
  * Entrypoint: `["/app/smarthome-metrics"]`.

---

## 6. GitHub Actions CI/CD Pipeline

### 6.1 Release Workflow (`.github/workflows/release.yml`)
* **Trigger:** Push of any tag matching `v*` (e.g. `v1.0.0`).
* **Environment Secrets:**
  * `DOCKERHUB_USERNAME`
  * `DOCKERHUB_TOKEN`
* **Jobs:**
  1. **Test:**
     * Runs `go test -v -race ./...`.
  2. **Build & Push Docker Image:**
     * Sets up QEMU for multi-platform build (`linux/amd64,linux/arm64`).
     * Sets up Docker Buildx.
     * Logs in to Docker Hub using secrets.
     * Generates Docker tags via `docker/metadata-action` (`bartlomiejklimczak/smarthome-metrics` with semver and latest tags).
     * Builds and pushes the image.
  3. **GitHub Release:**
     * Uses `softprops/action-gh-release@v2`.
     * Automatically extracts changelog and generates release notes.

### 6.2 CI Workflow (`.github/workflows/ci.yml`)
* **Trigger:** Pushes to `main` branch and Pull Requests.
* **Jobs:**
  * Runs `go test -v ./...`.
  * Runs `docker build .` to ensure Dockerfile builds without errors.

---

## 7. Kubernetes Helm Chart (`charts/smarthome-metrics`)

### 7.1 Chart Artifacts
* `Chart.yaml`: API version v2, version `0.1.0`, appVersion `1.0.0`.
* `values.yaml`:
  * `replicaCount: 1`
  * `image.repository: bartlomiejklimczak/smarthome-metrics`
  * `image.tag: ""` (defaults to `.Chart.AppVersion`)
  * `image.pullPolicy: IfNotPresent`
  * `service.type: ClusterIP`
  * `service.port: 8088`
  * `database.host: "postgres.default.svc.cluster.local"`
  * `database.port: 5432`
  * `database.name: "metrics"`
  * `database.user: "postgres"`
  * `database.password: ""`
  * `database.sslmode: "disable"`
  * `database.existingSecret: ""`
  * `database.existingSecretPasswordKey: "postgresql-password"`
  * `database.urlSecretKey: ""`
  * `resources`:
    * `requests: {cpu: "20m", memory: "32Mi"}`
    * `limits: {cpu: "200m", memory: "128Mi"}`
  * `livenessProbe`: HTTP GET `/healthz` on port 8088
  * `readinessProbe`: HTTP GET `/healthz` on port 8088
  * `securityContext`:
    * `runAsNonRoot: true`
    * `runAsUser: 10001`
    * `readOnlyRootFilesystem: true`
    * `capabilities: { drop: ["ALL"] }`
* Templates:
  * `deployment.yaml`, `service.yaml`, `configmap.yaml`, `secret.yaml`, `serviceaccount.yaml`, `ingress.yaml`, `_helpers.tpl`, `NOTES.txt`.

---

## 8. Verification & Testing Strategy
1. **Unit & Handler Testing:**
   * Handlers tested with `net/http/httptest` using a mock `MetricStore` interface.
   * Path parsing, validation (bad characters, empty parameters), and float parsing (valid floats, invalid string values, trailing spaces).
2. **Configuration Tests:**
   * Verify environment variable parsing and URL assembly.
3. **Helm Verification:**
   * Execute `helm lint charts/smarthome-metrics`.
   * Execute `helm template test charts/smarthome-metrics` with various `values.yaml` overrides to confirm manifests render cleanly.
4. **Container Build Verification:**
   * Build container locally using `docker build -t smarthome-metrics:test .`.
