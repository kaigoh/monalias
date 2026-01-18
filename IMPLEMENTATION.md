# Monalias Implementation Notes (v0.1)

This document describes how this repository implements the Monalias lookup protocol so third parties can match behavior.

## Public endpoints

- `GET /.well-known/monalias`
- `POST /_monalias/resolve`
- `GET /healthz`

See `internal/http/public.go`.

## Resolve behavior

Input:

```json
{
  "acct": "local$domain",
  "network": "mainnet"
}
```

Validation:

- `acct` must match the configured domain.
- `network` must be `mainnet` or `stagenet`.
- Instance status must not be `LOCKED`.

Lookup order:

1. Exact match in `aliases.full_acct`.
2. If no match and `MONALIAS_CATCHALL_ADDRESS` is set, return the catch-all address with `meta.resolved_kind = CATCH_ALL`.
3. Otherwise return `alias_not_found`.

Response fields:

- `address` is either the static address or a stored subaddress.
- `meta.alias` is the alias label from the `aliases` table.
- `meta.display_name` is derived from the local part (before `+` and `$`).
- `meta.resolved_kind` is `NORMAL` or `CATCH_ALL`.
- `expires_at` is omitted in v1.

## Signature

The server signs responses with Ed25519 using the configured private key.

Canonical string (newline separated):

```
MONALIAS_RESOLVE
<acct>
<address>
<network>
<expires_at_or_empty>
<key_id>
```

Headers:

- `X-Monalias-Key-Id`: key id (`kid`)
- `X-Monalias-Sig`: base64 signature

## Rate limiting

Per-IP token bucket on `/_monalias/resolve`:

- `MONALIAS_RATE_IP_RPS` (default `1.0`)
- `MONALIAS_RATE_IP_BURST` (default `10`)

On limit, response is `429` with a JSON body and `Retry-After: 30`.

See `internal/http/rate_limit.go`.

## Identity watchdog

The watchdog periodically fetches:

```
https://<MONALIAS_DOMAIN>/.well-known/monalias
```

It verifies:

- `homeserver` matches `MONALIAS_PUBLIC_BASE_URL`
- a key matches `signing_key_id` + `signing_pubkey`

If mismatched: status is set to `LOCKED` with reason `identity_mismatch`.
If unreachable: status is set to `DEGRADED` with reason `well_known_unreachable`.

When `LOCKED`, `/_monalias/resolve` returns `503`.

See `internal/identity/watchdog.go`.

## Storage

SQLite schema:

- `internal/db/schema.sql`
- `instance_config` contains public metadata and instance status.
- `accounts` stores account handles and optional wallet name.
- `aliases` stores alias resolution behavior.

The server initializes schema on startup and upserts `instance_config`.

## Wallet RPC (dynamic aliases)

Dynamic aliases use `monero-wallet-rpc` for view-only wallets:

- On alias creation, the admin API calls `open_wallet` and `create_address`.
- The resulting subaddress and index are stored on the alias.
- Resolve uses the stored index and `get_address`.

RPC code lives in `internal/monero/wallet_rpc.go`.

## Admin API

The admin API is GraphQL on the private listener, protected by HTTP basic auth.

Schema: `internal/graphql/schema.graphqls`
Handler: `internal/graphql/server.go`

## Embedded admin UI

The Flutter web build is embedded in the Go binary and served at `/` on the admin listener.

- Flutter source: `ui/`
- Embedded assets: `internal/ui/dist`
