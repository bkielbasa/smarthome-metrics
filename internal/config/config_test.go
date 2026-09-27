package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear relevant env vars
	envKeys := []string{"PORT", "DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE", "SHUTDOWN_TIMEOUT"}
	for _, k := range envKeys {
		os.Unsetenv(k)
	}

	cfg := Load()

	if cfg.Port != "8088" {
		t.Errorf("expected Port 8088, got %s", cfg.Port)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("expected DBHost localhost, got %s", cfg.DBHost)
	}
	if cfg.DBPort != "5432" {
		t.Errorf("expected DBPort 5432, got %s", cfg.DBPort)
	}
	if cfg.DBUser != "postgres" {
		t.Errorf("expected DBUser postgres, got %s", cfg.DBUser)
	}
	if cfg.DBName != "metrics" {
		t.Errorf("expected DBName metrics, got %s", cfg.DBName)
	}
	if cfg.DBSSLMode != "disable" {
		t.Errorf("expected DBSSLMode disable, got %s", cfg.DBSSLMode)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("expected ShutdownTimeout 5s, got %v", cfg.ShutdownTimeout)
	}

	expectedURL := "postgres://postgres@localhost:5432/metrics?sslmode=disable"
	if cfg.GetDatabaseURL() != expectedURL {
		t.Errorf("expected GetDatabaseURL %s, got %s", expectedURL, cfg.GetDatabaseURL())
	}
}

func TestLoadConfig_CustomEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DATABASE_URL", "postgres://custom:pass@remote:5433/customdb?sslmode=require")
	os.Setenv("SHUTDOWN_TIMEOUT", "10s")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("SHUTDOWN_TIMEOUT")
	}()

	cfg := Load()

	if cfg.Port != "9090" {
		t.Errorf("expected Port 9090, got %s", cfg.Port)
	}
	if cfg.GetDatabaseURL() != "postgres://custom:pass@remote:5433/customdb?sslmode=require" {
		t.Errorf("expected custom DATABASE_URL, got %s", cfg.GetDatabaseURL())
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
}

func TestLoadConfig_WithDBPassword(t *testing.T) {
	os.Setenv("DB_PASSWORD", "secret")
	defer os.Unsetenv("DB_PASSWORD")

	cfg := Load()
	expectedURL := "postgres://postgres:secret@localhost:5432/metrics?sslmode=disable"
	if cfg.GetDatabaseURL() != expectedURL {
		t.Errorf("expected GetDatabaseURL %s, got %s", expectedURL, cfg.GetDatabaseURL())
	}
}

func TestLoadConfig_InvalidShutdownTimeout(t *testing.T) {
	os.Setenv("SHUTDOWN_TIMEOUT", "invalid-duration")
	defer os.Unsetenv("SHUTDOWN_TIMEOUT")

	cfg := Load()
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("expected default ShutdownTimeout 5s on error, got %v", cfg.ShutdownTimeout)
	}
}

