package main

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kaigoh/monalias/internal/config"
	"github.com/kaigoh/monalias/internal/db"
	"github.com/kaigoh/monalias/internal/graphql"
	httpx "github.com/kaigoh/monalias/internal/http"
	"github.com/kaigoh/monalias/internal/identity"
	"github.com/kaigoh/monalias/internal/monero"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	signer, pubkey, err := readSigningKey(cfg.SigningKeyFile)
	if err != nil {
		log.Fatalf("signing key error: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db open error: %v", err)
	}
	defer database.Close()

	if err := initSchema(database); err != nil {
		log.Fatalf("db schema error: %v", err)
	}

	if err := ensureInstanceConfig(database, cfg, pubkey); err != nil {
		log.Fatalf("instance config error: %v", err)
	}

	var walletRPC *monero.WalletRPC
	if cfg.WalletRPCURL != "" {
		walletRPC = monero.NewWalletRPC(cfg.WalletRPCURL, cfg.WalletRPCUser, cfg.WalletRPCPass)
	}

	watchdog := identity.New(cfg, database)

	gqlHandler, err := graphql.NewHandler(cfg, database, walletRPC, watchdog)
	if err != nil {
		log.Fatalf("graphql error: %v", err)
	}

	adminHandler := httpx.AdminHandler(cfg, gqlHandler)
	publicSvc := httpx.NewPublicService(cfg, database, signer, walletRPC)
	limiter := httpx.NewIPRateLimiter(cfg.RateRPS, cfg.RateBurst)
	publicHandler := publicSvc.Handler(limiter)

	publicServer := &http.Server{
		Addr:              cfg.PublicBind,
		Handler:           publicHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	adminServer := &http.Server{
		Addr:              cfg.AdminBind,
		Handler:           adminHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go watchdog.Run(ctx, cfg.IdentityInterval)

	go func() {
		log.Printf("public listener on %s", cfg.PublicBind)
		if err := publicServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("public server error: %v", err)
			stop()
		}
	}()

	go func() {
		log.Printf("admin listener on %s", cfg.AdminBind)
		if err := adminServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("admin server error: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = publicServer.Shutdown(shutdownCtx)
	_ = adminServer.Shutdown(shutdownCtx)
}

func initSchema(database *db.DB) error {
	schema, err := db.Schema()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return database.InitSchema(ctx, schema)
}

func ensureInstanceConfig(database *db.DB, cfg config.Config, pubkey string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	current, err := database.GetInstanceConfig(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	status := current.Status
	if status == "" {
		status = "OK"
	}

	_, err = database.UpsertInstanceConfig(
		ctx,
		cfg.Domain,
		cfg.PublicBaseURL,
		cfg.SigningKeyID,
		pubkey,
		status,
		current.StatusReason,
		current.LastIdentityCheckAt,
	)
	return err
}

func readSigningKey(path string) (ed25519.PrivateKey, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	trimmed := strings.TrimSpace(string(data))
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		raw = []byte(trimmed)
	}

	var priv ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(raw)
	default:
		return nil, "", errors.New("signing key must be 32-byte seed or 64-byte private key")
	}

	pub := priv.Public().(ed25519.PublicKey)
	return priv, base64.StdEncoding.EncodeToString(pub), nil
}
