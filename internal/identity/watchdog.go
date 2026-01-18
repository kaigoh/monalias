package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kaigoh/monalias/internal/config"
	"github.com/kaigoh/monalias/internal/db"
)

type Watchdog struct {
	cfg    config.Config
	db     *db.DB
	client *http.Client
}

func New(cfg config.Config, database *db.DB) *Watchdog {
	return &Watchdog{
		cfg: cfg,
		db:  database,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (w *Watchdog) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = w.CheckOnce(ctx)
		}
	}
}

type wellKnown struct {
	Homeserver string `json:"homeserver"`
	Version    string `json:"version"`
	Keys       []struct {
		Kid       string `json:"kid"`
		Alg       string `json:"alg"`
		PublicKey string `json:"public_key"`
		Use       string `json:"use"`
	} `json:"keys"`
}

func (w *Watchdog) CheckOnce(ctx context.Context) (db.InstanceConfig, error) {
	cfg, err := w.db.GetInstanceConfig(ctx)
	if err != nil {
		return cfg, err
	}

	wellKnownURL := fmt.Sprintf("https://%s/.well-known/monalias", w.cfg.Domain)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return cfg, err
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return w.markStatus(ctx, "DEGRADED", sql.NullString{String: "well_known_unreachable", Valid: true})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return w.markStatus(ctx, "DEGRADED", sql.NullString{String: "well_known_unreachable", Valid: true})
	}

	var wk wellKnown
	if err := json.NewDecoder(resp.Body).Decode(&wk); err != nil {
		return w.markStatus(ctx, "DEGRADED", sql.NullString{String: "well_known_unreachable", Valid: true})
	}

	if wk.Homeserver != w.cfg.PublicBaseURL {
		return w.markStatus(ctx, "LOCKED", sql.NullString{String: "identity_mismatch", Valid: true})
	}

	if !keyMatches(wk.Keys, cfg.SigningKeyID, cfg.SigningPubkey) {
		return w.markStatus(ctx, "LOCKED", sql.NullString{String: "identity_mismatch", Valid: true})
	}

	return w.markStatus(ctx, "OK", sql.NullString{Valid: false})
}

func keyMatches(keys []struct {
	Kid       string `json:"kid"`
	Alg       string `json:"alg"`
	PublicKey string `json:"public_key"`
	Use       string `json:"use"`
}, kid, pubkey string) bool {
	for _, key := range keys {
		if key.Kid == kid && key.PublicKey == pubkey {
			return true
		}
	}
	return false
}

func (w *Watchdog) markStatus(ctx context.Context, status string, reason sql.NullString) (db.InstanceConfig, error) {
	cfg, err := w.db.UpdateInstanceStatus(ctx, status, reason, sql.NullTime{Time: time.Now().UTC(), Valid: true})
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ValidateConfig(cfg db.InstanceConfig) error {
	if cfg.Domain == "" || cfg.Homeserver == "" || cfg.SigningKeyID == "" || cfg.SigningPubkey == "" {
		return errors.New("instance config incomplete")
	}
	return nil
}
