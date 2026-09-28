package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                         string
	DatabaseURL                  string
	DBHost                       string
	DBPort                       string
	DBUser                       string
	DBPassword                   string
	DBName                       string
	DBSSLMode                    string
	ShutdownTimeout              time.Duration
	PSEEnabled                   bool
	PSEFetchInterval             time.Duration
	PSEApiURL                    string
	SimulationEnabled            bool
	SimulationInterval           time.Duration
	SimulationBatteryCapacityKWh float64
	SimulationBatteryPowerKW     float64
	SimulationDistributionFee    float64
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultValue
}

func Load() *Config {
	shutdownTimeoutStr := getEnv("SHUTDOWN_TIMEOUT", "5s")
	shutdownTimeout, err := time.ParseDuration(shutdownTimeoutStr)
	if err != nil {
		shutdownTimeout = 5 * time.Second
	}

	pseEnabledStr := getEnv("PSE_ENABLED", "true")
	pseEnabled := pseEnabledStr != "false" && pseEnabledStr != "0"

	pseFetchIntervalStr := getEnv("PSE_FETCH_INTERVAL", "15m")
	pseFetchInterval, err := time.ParseDuration(pseFetchIntervalStr)
	if err != nil {
		pseFetchInterval = 15 * time.Minute
	}

	simEnabled := true
	if val := os.Getenv("SIMULATION_ENABLED"); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			simEnabled = parsed
		}
	}

	simInterval := time.Minute
	if val := os.Getenv("SIMULATION_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			simInterval = d
		}
	}

	simCapacity := 10.0
	if val := os.Getenv("SIMULATION_BATTERY_CAPACITY_KWH"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
			simCapacity = f
		}
	}

	simPower := 5.0
	if val := os.Getenv("SIMULATION_BATTERY_POWER_KW"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f > 0 {
			simPower = f
		}
	}

	simFee := 0.40
	if val := os.Getenv("SIMULATION_DISTRIBUTION_FEE"); val != "" {
		if f, err := strconv.ParseFloat(val, 64); err == nil && f >= 0 {
			simFee = f
		}
	}

	return &Config{
		Port:                         getEnv("PORT", "8088"),
		DatabaseURL:                  getEnv("DATABASE_URL", ""),
		DBHost:                       getEnv("DB_HOST", "localhost"),
		DBPort:                       getEnv("DB_PORT", "5432"),
		DBUser:                       getEnv("DB_USER", "postgres"),
		DBPassword:                   getEnv("DB_PASSWORD", ""),
		DBName:                       getEnv("DB_NAME", "metrics"),
		DBSSLMode:                    getEnv("DB_SSLMODE", "disable"),
		ShutdownTimeout:              shutdownTimeout,
		PSEEnabled:                   pseEnabled,
		PSEFetchInterval:             pseFetchInterval,
		PSEApiURL:                    getEnv("PSE_API_URL", "https://api.raporty.pse.pl/api/rce-pln"),
		SimulationEnabled:            simEnabled,
		SimulationInterval:           simInterval,
		SimulationBatteryCapacityKWh: simCapacity,
		SimulationBatteryPowerKW:     simPower,
		SimulationDistributionFee:    simFee,
	}
}

func (c *Config) GetDatabaseURL() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}

	var userPass *url.Userinfo
	if c.DBPassword != "" {
		userPass = url.UserPassword(c.DBUser, c.DBPassword)
	} else {
		userPass = url.User(c.DBUser)
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     userPass,
		Host:     fmt.Sprintf("%s:%s", c.DBHost, c.DBPort),
		Path:     c.DBName,
		RawQuery: fmt.Sprintf("sslmode=%s", url.QueryEscape(c.DBSSLMode)),
	}

	return u.String()
}
