# Monalias v1

![Monalias logo](monalias.png)

Monalias v1 is a small, self-hosted service that resolves DNS-style aliases into Monero addresses and signs the responses. It exposes a public resolver and a private admin API with a bundled admin UI.

## Quick start

1. Create a `.env` file from the example:

```bash
cp .env.example .env
```

2. Generate a 32-byte seed and store it as base64:

```bash
openssl rand -base64 32 > ./secrets/monalias_signing_key
```

3. Build and run:

```bash
docker compose up --build
```

4. Reach the admin API via SSH tunnel (do not expose port 8080 directly):

```bash
ssh -L 8080:127.0.0.1:8080 user@server
```

## Public endpoints

- `GET /.well-known/monalias`
- `POST /_monalias/resolve`
- `GET /healthz`

## Admin API

- `POST /graphql` on the admin listener (default `127.0.0.1:8080`)
- HTTP basic auth via `MONALIAS_ADMIN_USER` / `MONALIAS_ADMIN_PASSWORD`

The admin UI is bundled into the Go binary and served at `/` on the admin listener.

## Configuration

Core settings are read from `.env` or environment variables:

- `MONALIAS_DOMAIN`
- `MONALIAS_PUBLIC_BASE_URL`
- `MONALIAS_DB_PATH`
- `MONALIAS_RATE_IP_RPS`
- `MONALIAS_RATE_IP_BURST`
- `MONALIAS_CATCHALL_ADDRESS`
- `MONALIAS_WALLET_RPC_URL`
- `MONALIAS_WALLET_RPC_USER`
- `MONALIAS_WALLET_RPC_PASSWORD`
- `MONALIAS_SIGNING_KEY_FILE`
- `MONALIAS_SIGNING_KEY_ID`
- `MONALIAS_ADMIN_USER`
- `MONALIAS_ADMIN_PASSWORD`

## Signing key format

`MONALIAS_SIGNING_KEY_FILE` must contain either:

- base64-encoded 32-byte Ed25519 seed, or
- base64-encoded 64-byte Ed25519 private key

The server derives the public key and stores it in `instance_config` for `.well-known/monalias`.

## Database

SQLite schema lives in `internal/db/schema.sql`. The service creates tables on boot and maintains a single-row `instance_config` entry.

## Wallet RPC

Dynamic aliases require `monero-wallet-rpc` with view-only wallets. The service will `open_wallet` and derive a subaddress index during alias creation, and resolve using that stored index.

## Development

- Go entrypoint: `cmd/monalias/main.go`
- Public HTTP: `internal/http/public.go`
- Admin HTTP: `internal/http/admin.go`
- GraphQL: `internal/graphql`
- Monero RPC: `internal/monero`
- Identity watchdog: `internal/identity`
- Embedded UI: `internal/ui`
