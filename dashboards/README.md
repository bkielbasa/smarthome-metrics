# Grafana Dashboards

This directory contains pre-configured Grafana dashboards for `smarthome-metrics`:

- **[`smarthome-energia.json`](./smarthome-energia.json)**: Energy consumption and power metrics (per phase, daily totals, and real-time usage).
- **[`smarthome-pokoje.json`](./smarthome-pokoje.json)**: Room climate metrics (temperature, humidity, pressure across living areas).

---

## Automatic Sync with Grafana in Kubernetes

To load and sync these dashboards automatically directly from GitHub without rebuilding images or creating manual ConfigMaps, deploy Grafana using the official Helm chart with the provided [`deploy/grafana/values-git-sync.yaml`](../deploy/grafana/values-git-sync.yaml) values.

### 1. Deploy / Upgrade Grafana

```bash
# Add the official Grafana Helm repository
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Install or upgrade Grafana using the git-sync values
helm upgrade --install grafana grafana/grafana \
  --namespace monitoring --create-namespace \
  -f deploy/grafana/values-git-sync.yaml
```

### 2. How It Works

1. A lightweight **`git-sync`** sidecar container runs alongside Grafana and periodically (every 30s) checks `https://github.com/bkielbasa/smarthome-metrics.git` on the `master` branch.
2. Synced repository files are written to a shared `emptyDir` volume at `/var/lib/grafana/dashboards-git/current`.
3. Grafana's built-in **`dashboardProviders`** watches `/var/lib/grafana/dashboards-git/current/dashboards` and reloads any changes every 30s with zero downtime.

### 3. Datasource Note

The dashboards are configured to query the PostgreSQL datasource with UID `dfzk58ktrrnr4f`. Ensure your Grafana PostgreSQL datasource is provisioned or configured with this UID.
