package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultPublicBind = ":80"
	defaultAdminBind  = "127.0.0.1:8080"
)

// Config holds all runtime configuration values.
type Config struct {
	Domain           string
	PublicBaseURL    string
	DBPath           string
	RateRPS          float64
	RateBurst        int
	CatchAllAddress  string
	WalletRPCURL     string
	WalletRPCUser    string
	WalletRPCPass    string
	SigningKeyFile   string
	SigningKeyID     string
	AdminUser        string
	AdminPassword    string
	PublicBind       string
	AdminBind        string
	IdentityInterval time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		Domain:           os.Getenv("MONALIAS_DOMAIN"),
		PublicBaseURL:    os.Getenv("MONALIAS_PUBLIC_BASE_URL"),
		DBPath:           getenvDefault("MONALIAS_DB_PATH", "./monalias.db"),
		RateRPS:          getenvFloat("MONALIAS_RATE_IP_RPS", 1.0),
		RateBurst:        getenvInt("MONALIAS_RATE_IP_BURST", 10),
		CatchAllAddress:  os.Getenv("MONALIAS_CATCHALL_ADDRESS"),
		WalletRPCURL:     os.Getenv("MONALIAS_WALLET_RPC_URL"),
		WalletRPCUser:    os.Getenv("MONALIAS_WALLET_RPC_USER"),
		WalletRPCPass:    os.Getenv("MONALIAS_WALLET_RPC_PASSWORD"),
		SigningKeyFile:   os.Getenv("MONALIAS_SIGNING_KEY_FILE"),
		SigningKeyID:     getenvDefault("MONALIAS_SIGNING_KEY_ID", "main-2026-01"),
		AdminUser:        getenvDefault("MONALIAS_ADMIN_USER", "admin"),
		AdminPassword:    os.Getenv("MONALIAS_ADMIN_PASSWORD"),
		PublicBind:       getenvDefault("MONALIAS_PUBLIC_BIND", defaultPublicBind),
		AdminBind:        getenvDefault("MONALIAS_ADMIN_BIND", defaultAdminBind),
		IdentityInterval: getenvDuration("MONALIAS_IDENTITY_INTERVAL", 15*time.Minute),
	}

	if cfg.SigningKeyFile == "" {
		secretPath := "/run/secrets/monalias_signing_key"
		if fileExists(secretPath) {
			cfg.SigningKeyFile = secretPath
		}
	}

	if cfg.WalletRPCPass == "" {
		secretPath := "/run/secrets/wallet_rpc_password"
		if fileExists(secretPath) {
			data, err := os.ReadFile(secretPath)
			if err == nil {
				cfg.WalletRPCPass = string(trimSpace(data))
			}
		}
	}

	if cfg.Domain == "" {
		return cfg, errors.New("MONALIAS_DOMAIN is required")
	}
	if cfg.PublicBaseURL == "" {
		return cfg, errors.New("MONALIAS_PUBLIC_BASE_URL is required")
	}
	if cfg.SigningKeyFile == "" {
		return cfg, errors.New("MONALIAS_SIGNING_KEY_FILE or /run/secrets/monalias_signing_key is required")
	}
	if cfg.AdminPassword == "" {
		return cfg, errors.New("MONALIAS_ADMIN_PASSWORD is required")
	}

	cfg.DBPath = filepath.Clean(cfg.DBPath)
	return cfg, nil
}

func getenvDefault(key, val string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return val
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return def
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func trimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func (c Config) String() string {
	return fmt.Sprintf("domain=%s public_base_url=%s db=%s", c.Domain, c.PublicBaseURL, c.DBPath)
}
