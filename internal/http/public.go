package httpx

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kaigoh/monalias/internal/config"
	"github.com/kaigoh/monalias/internal/db"
	"github.com/kaigoh/monalias/internal/monero"
)

type PublicService struct {
	cfg        config.Config
	db         *db.DB
	signer     ed25519.PrivateKey
	walletRPC  *monero.WalletRPC
	catchAll   string
	signingKID string
}

func NewPublicService(cfg config.Config, database *db.DB, signer ed25519.PrivateKey, walletRPC *monero.WalletRPC) *PublicService {
	return &PublicService{
		cfg:        cfg,
		db:         database,
		signer:     signer,
		walletRPC:  walletRPC,
		catchAll:   cfg.CatchAllAddress,
		signingKID: cfg.SigningKeyID,
	}
}

func (s *PublicService) Handler(limiter *IPRateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/monalias", s.handleWellKnown)
	if limiter != nil {
		mux.Handle("/_monalias/resolve", limiter.Middleware(http.HandlerFunc(s.handleResolve)))
	} else {
		mux.HandleFunc("/_monalias/resolve", s.handleResolve)
	}
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *PublicService) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *PublicService) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg, err := s.db.GetInstanceConfig(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error")
		return
	}

	resp := map[string]interface{}{
		"homeserver": cfg.Homeserver,
		"version":    "0.1",
		"keys": []map[string]string{
			{
				"kid":        cfg.SigningKeyID,
				"alg":        "Ed25519",
				"public_key": cfg.SigningPubkey,
				"use":        "sig",
			},
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

type resolveRequest struct {
	Acct    string `json:"acct"`
	Network string `json:"network"`
}

type resolveResponse struct {
	Address   string      `json:"address"`
	Network   string      `json:"network"`
	Meta      resolveMeta `json:"meta"`
	ExpiresAt *time.Time  `json:"expires_at,omitempty"`
}

type resolveMeta struct {
	DisplayName  *string `json:"display_name"`
	Alias        *string `json:"alias"`
	ResolvedKind string  `json:"resolved_kind"`
}

func (s *PublicService) handleResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	instCfg, err := s.db.GetInstanceConfig(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error")
		return
	}
	if instCfg.Status == "LOCKED" {
		writeJSONErrorWithReason(w, http.StatusServiceUnavailable, "instance_locked", instCfg.StatusReason)
		return
	}

	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Acct == "" || req.Network == "" {
		writeJSONError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Network != "mainnet" && req.Network != "stagenet" {
		writeJSONError(w, http.StatusBadRequest, "invalid_network")
		return
	}
	if !acctMatchesDomain(req.Acct, s.cfg.Domain) {
		writeJSONError(w, http.StatusNotFound, "alias_not_found")
		return
	}

	alias, err := s.db.GetAliasByFullAcct(ctx, req.Acct)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.handleCatchAll(w, req)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "server_error")
		return
	}

	address, aliasLabel, resolvedKind, err := s.resolveAlias(ctx, alias)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error")
		return
	}

	resp := resolveResponse{
		Address: address,
		Network: req.Network,
		Meta: resolveMeta{
			Alias:        aliasLabel,
			ResolvedKind: resolvedKind,
		},
	}

	if display := displayNameFromAcct(req.Acct); display != "" {
		resp.Meta.DisplayName = &display
	}

	s.signResolveResponse(w, req, resp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *PublicService) resolveAlias(ctx context.Context, alias db.Alias) (string, *string, string, error) {
	label := alias.AliasLabel
	resolvedKind := "NORMAL"

	if alias.Mode == "STATIC_ADDRESS" {
		if !alias.StaticAddress.Valid || alias.StaticAddress.String == "" {
			return "", nil, "", errors.New("static address missing")
		}
		return alias.StaticAddress.String, &label, resolvedKind, nil
	}

	if alias.Mode == "DYNAMIC_SUBADDRESS" {
		if alias.StaticAddress.Valid && alias.StaticAddress.String != "" {
			return alias.StaticAddress.String, &label, resolvedKind, nil
		}
		if !alias.NextSubaddrIdx.Valid {
			return "", nil, "", errors.New("dynamic alias missing index")
		}
		if s.walletRPC == nil || !s.walletRPC.Enabled() {
			return "", nil, "", errors.New("wallet rpc not configured")
		}
		acct, err := s.db.GetAccount(ctx, alias.AccountID)
		if err != nil {
			return "", nil, "", err
		}
		if !acct.WalletName.Valid || acct.WalletName.String == "" {
			return "", nil, "", errors.New("wallet name missing")
		}
		if err := s.walletRPC.OpenWallet(ctx, acct.WalletName.String); err != nil {
			return "", nil, "", err
		}
		addr, err := s.walletRPC.GetAddress(ctx, alias.NextSubaddrIdx.Int64)
		if err != nil {
			return "", nil, "", err
		}
		return addr, &label, resolvedKind, nil
	}

	return "", nil, "", errors.New("unknown alias mode")
}

func (s *PublicService) handleCatchAll(w http.ResponseWriter, req resolveRequest) {
	if s.catchAll == "" {
		writeJSONError(w, http.StatusNotFound, "alias_not_found")
		return
	}

	resp := resolveResponse{
		Address: s.catchAll,
		Network: req.Network,
		Meta: resolveMeta{
			ResolvedKind: "CATCH_ALL",
		},
	}

	if display := fmt.Sprintf("%s (catch-all)", s.cfg.Domain); display != "" {
		resp.Meta.DisplayName = &display
	}

	s.signResolveResponse(w, req, resp)
	writeJSON(w, http.StatusOK, resp)
}

func (s *PublicService) signResolveResponse(w http.ResponseWriter, req resolveRequest, resp resolveResponse) {
	expires := ""
	if resp.ExpiresAt != nil {
		expires = resp.ExpiresAt.UTC().Format(time.RFC3339)
	}

	canonical := strings.Join([]string{
		"MONALIAS_RESOLVE",
		req.Acct,
		resp.Address,
		req.Network,
		expires,
		s.signingKID,
	}, "\n")

	sig := ed25519.Sign(s.signer, []byte(canonical))
	w.Header().Set("X-Monalias-Key-Id", s.signingKID)
	w.Header().Set("X-Monalias-Sig", base64.StdEncoding.EncodeToString(sig))
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]interface{}{"error": code})
}

func writeJSONErrorWithReason(w http.ResponseWriter, status int, code string, reason sql.NullString) {
	payload := map[string]interface{}{"error": code}
	if reason.Valid {
		payload["reason"] = reason.String
	}
	writeJSON(w, status, payload)
}

func acctMatchesDomain(acct, domain string) bool {
	parts := strings.Split(acct, "$")
	if len(parts) != 2 {
		return false
	}
	return strings.EqualFold(parts[1], domain)
}

func displayNameFromAcct(acct string) string {
	parts := strings.Split(acct, "$")
	if len(parts) == 0 {
		return ""
	}
	local := parts[0]
	if local == "" {
		return ""
	}
	if idx := strings.Index(local, "+"); idx >= 0 {
		local = local[:idx]
	}
	return local
}
