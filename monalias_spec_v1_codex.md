# Monalias v1 – Implementation Spec (Codex)

Monalias v1 is a small, self‑hosted service that provides **DNS‑style aliases for Monero wallets**, with optional view‑only wallet support for generating subaddresses.

This document is aimed at **Codex / implementation** and is intentionally concrete:

- **Backend:** Go (golang), `sqlc`, `gqlgen`
- **Storage:** SQLite
- **Monero integration:** `monero-wallet-rpc` (view‑only wallets only)
- **Admin UI:** Flutter (web), bundled into the Go binary via `embed.FS`
- **Deploy:** Docker + docker‑compose
- **Config:** `.env` + environment variables, sensitive stuff via Docker secrets

---

## 1. High‑level behaviour

Monalias v1 does **one public thing**:

> Given a Monalias ID like `bob$example.com`, return a Monero address plus some metadata, signed by the domain’s Monalias server key.

Internally, an admin can:

- Define **accounts** (e.g. `bob$example.com`)
- Attach **view‑only wallets** to accounts (optional but supported)
- Define **aliases** per account (e.g. `bob+rent$example.com`)
- Choose per‑alias behaviour:
  - `STATIC_ADDRESS` – a fixed Monero address (OpenAlias‑style)
  - `DYNAMIC_SUBADDRESS` – addresses derived from a view‑only wallet

Out of scope for v1:

- No spend keys in the backend
- No sending transactions
- No balance tracking
- No public registration or “user auth” for end‑users

---

## 2. Monalias ID format

### 2.1 Human format

A **Monalias ID** is:

```text
local_part$domain
```

Examples:

- `bob$example.com`
- `bob+rent$example.com`
- `shop+coffee$cafe.example.fr`

Notes:

- `$` is the separator (not `@`) to avoid confusion with email.
- `+alias` is optional, and can be used as a label or bucket (`rent`, `coffee`, etc.).

### 2.2 Canonical URI form

For QR codes, deep links, etc., use:

```text
xmr:local_part$domain
```

Example:

- `xmr:bob$example.com`

The backend does **not** need to parse the `xmr:` prefix; that’s a wallet/UX concern.

---

## 3. Public protocol

Monalias has a very small public surface:

- `GET /.well-known/monalias`
- `POST /_monalias/resolve`
- `GET /healthz` (optional, for uptime checks)

### 3.1 Well‑known metadata

Each Monalias domain **must** serve:

```http
GET https://<domain>/.well-known/monalias
```

Response (JSON):

```json
{
  "homeserver": "https://monalias.example.com",
  "version": "0.1",
  "keys": [
    {
      "kid": "main-2026-01",
      "alg": "Ed25519",
      "public_key": "BASE64_ED25519_PUBLIC_KEY",
      "use": "sig"
    }
  ]
}
```

Fields:

- `homeserver` – Public base URL that exposes `/_monalias/resolve`.
- `version` – Protocol version, `"0.1"` for this spec.
- `keys` – One or more signing keys:
  - `kid` – key ID (string)
  - `alg` – algorithm, `"Ed25519"`
  - `public_key` – base64‑encoded public key
  - `use` – currently `"sig"`

The backend should read these values from config / DB (`instance_config` table) and render this JSON.

### 3.2 SRV fallback (optional)

Domains **may** publish a SRV record:

```text
_monalias._tcp.<domain>  IN  SRV  priority weight port target
```

Example:

```text
_monalias._tcp.example.com.  3600 IN SRV 10 5 443 monalias.example.com.
```

Client behaviour:

1. Try `GET https://<domain>/.well-known/monalias`.
2. If that fails, resolve `_monalias._tcp.<domain>` and talk to the SRV host:port.

The Monalias server does not need to query SRV itself except in its **identity watchdog** (see later).

### 3.3 Resolve endpoint

Public resolver endpoint on the homeserver:

```http
POST /_monalias/resolve
Content-Type: application/json
```

#### Request body

```json
{
  "acct": "bob+rent$example.com",
  "network": "mainnet"
}
```

Fields:

- `acct` – The Monalias ID (`local_part$domain`). **Required.**
- `network` – `"mainnet"` or `"stagenet"`. **Required.**  
  (No testnet support needed in v1, but implementation should be future‑proof.)

#### Success response (200 OK)

Headers:

```http
HTTP/1.1 200 OK
Content-Type: application/json
X-Monalias-Key-Id: main-2026-01
X-Monalias-Sig: BASE64_SIGNATURE
```

Body:

```json
{
  "address": "89MoneroAddressOrSubaddress...",
  "network": "mainnet",
  "meta": {
    "display_name": "Kai",
    "alias": "rent",
    "resolved_kind": "NORMAL"
  },
  "expires_at": "2026-01-18T11:30:00Z"
}
```

Fields:

- `address` – The Monero address / subaddress string.
- `network` – Echo of requested network.
- `meta.display_name` – Optional, for UI friendliness.
- `meta.alias` – Optional alias label, e.g. `"rent"`, `"default"`.
- `meta.resolved_kind` – Either `"NORMAL"` or `"CATCH_ALL"`.
- `expires_at` – Optional ISO8601 UTC timestamp. Advisory only.

#### Signature

Use Ed25519 with the private key corresponding to the `kid` in `.well-known/monalias`.

Canonical string to sign (v1):

```text
MONALIAS_RESOLVE
<acct>
<address>
<network>
<expires_at_or_empty>
<key_id>
```

- Newline separated (`\n`).
- `expires_at_or_empty` = `expires_at` string or empty if unset.
- `key_id` = the `kid` value.

Server behaviour:

1. Build canonical string from the **request**’s `acct` + `network` and the **response**’s `address` + `expires_at` + configured key id.
2. Sign with Ed25519 private key.
3. Base64‑encode signature into `X-Monalias-Sig` header.
4. Put `kid` into `X-Monalias-Key-Id`.

Client behaviour (for wallet devs, not Codex):

1. Fetch `.well-known/monalias` for the target domain.
2. Find key with matching `kid`.
3. Rebuild canonical string from the resolve response.
4. Verify Ed25519 signature using `public_key`.

#### Errors

**Unknown alias, no catch‑all:**

```http
HTTP/1.1 404 Not Found
Content-Type: application/json
```

```json
{
  "error": "alias_not_found"
}
```

**Rate limited (per IP):**

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 30
```

```json
{
  "error": "rate_limited",
  "retry_after_seconds": 30
}
```

**Instance locked (identity mismatch, admin lock):**

```http
HTTP/1.1 503 Service Unavailable
Content-Type: application/json
```

```json
{
  "error": "instance_locked",
  "reason": "identity_mismatch"
}
```

---

## 4. Rate limiting (v1)

Implement a **per‑IP token bucket** on `/_monalias/resolve`.

Default values (configurable):

- `MONALIAS_RATE_IP_RPS` – default `"1.0"` (1 request / second)
- `MONALIAS_RATE_IP_BURST` – default `"10"` (burst capacity)

If a request exceeds the limit:

- Respond with HTTP 429
- Include `Retry-After` header
- Include JSON body:
  ```json
  {
    "error": "rate_limited",
    "retry_after_seconds": 30
  }
  ```

Implementation in Go: use `golang.org/x/time/rate` and maintain a `map[ip]string]*rate.Limiter` with periodic cleanup of stale entries.

No per‑alias rate limits are required for v1.

---

## 5. Catch‑all behaviour

Admins can configure a **catch‑all address** for unknown aliases.

- Env var: `MONALIAS_CATCHALL_ADDRESS`
- If **unset / empty** (default): unknown aliases → 404 as above.
- If set:
  - Unknown `acct` values resolve to this address.
  - `meta.resolved_kind` MUST be `"CATCH_ALL"`.

Example success response for an unknown alias with catch‑all configured:

```json
{
  "address": "89CatchAllDonateAddr...",
  "network": "mainnet",
  "meta": {
    "display_name": "Example.com (catch-all)",
    "alias": null,
    "resolved_kind": "CATCH_ALL"
  },
  "expires_at": "2026-01-18T11:30:00Z"
}
```

Catch‑all should be **off by default**.

---

## 6. Internal architecture

### 6.1 Components

- **Monalias server (Go):**
  - Public HTTP listener (port 80) – `.well-known`, `/_monalias/resolve`, `healthz`
  - Private HTTP listener (port 8080, localhost) – GraphQL admin API
  - SQLite storage access (via `sqlc`)
  - Optional Monero integration via `monero-wallet-rpc` (view‑only)
  - Identity watchdog (poll `.well-known/monalias`)
  - Rate limiting on `/_monalias/resolve`
  - Static file serving for embedded Flutter admin UI (from `embed.FS`)

- **monero-wallet-rpc:**
  - Optional but recommended for `DYNAMIC_SUBADDRESS` aliases.
  - Started separately, using `--wallet-dir` to manage multiple view‑only wallets.

### 6.2 Listeners

- Public HTTP:
  - Bind address default: `:80`
  - Endpoints:
    - `GET /.well-known/monalias`
    - `POST /_monalias/resolve`
    - `GET /healthz`

- Admin HTTP:
  - Bind address default: `127.0.0.1:8080`
  - Endpoint:
    - `POST /graphql` (and optionally `GET /graphql` for GraphiQL)
  - Only accessible via localhost, VPN, or SSH tunnel.

Bind addresses should be configurable if necessary, but sane defaults are fine.

---

## 7. Data model (SQLite via sqlc)

### 7.1 Schema

#### `instance_config`

```sql
CREATE TABLE IF NOT EXISTS instance_config (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  domain TEXT NOT NULL,
  homeserver TEXT NOT NULL,
  signing_key_id TEXT NOT NULL,
  signing_pubkey TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'OK',     -- 'OK', 'DEGRADED', 'LOCKED'
  status_reason TEXT,
  last_identity_check_at DATETIME
);
```

Exactly one row (id = 1).

#### `accounts`

```sql
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  handle TEXT NOT NULL UNIQUE,           -- full account handle: "bob$example.com"
  wallet_name TEXT,                      -- name/path of view-only wallet in wallet-rpc's wallet-dir
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- `handle` corresponds to the account part (without `+alias`) and domain.

#### `aliases`

```sql
CREATE TABLE IF NOT EXISTS aliases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  full_acct TEXT NOT NULL UNIQUE,        -- "bob$example.com", "bob+rent$example.com"
  alias_label TEXT NOT NULL,             -- "default", "rent", etc.
  mode TEXT NOT NULL,                    -- 'STATIC_ADDRESS' | 'DYNAMIC_SUBADDRESS'
  static_address TEXT,                   -- used iff mode = 'STATIC_ADDRESS'
  next_subaddr_idx INTEGER,              -- used iff mode = 'DYNAMIC_SUBADDRESS'
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- For v1, `next_subaddr_idx` can be used either as:
  - a single fixed index, or
  - a rotating counter for per‑resolve subaddresses.
- `alias_label` is an internal label; `full_acct` is what appears externally.

### 7.2 sqlc

Provide a `sqlc.yaml` similar to:

```yaml
version: "2"
sql:
  - engine: "sqlite"
    schema: "internal/sql/schema.sql"
    queries: "internal/sql/queries.sql"
    gen:
      go:
        package: "db"
        out: "internal/db"
```

`schema.sql` contains the `CREATE TABLE` statements.  
`queries.sql` contains named queries for:

- Instance config (get/update)
- Accounts (CRUD)
- Aliases (CRUD, lookup by `full_acct`, etc.)

Example query snippet:

```sql
-- name: GetAliasByFullAcct :one
SELECT * FROM aliases WHERE full_acct = ?;

-- name: ListAccounts :many
SELECT * FROM accounts ORDER BY created_at;

-- name: ListAliasesForAccount :many
SELECT * FROM aliases WHERE account_id = ? ORDER BY created_at;
```

Codex should generate Go types + methods into `internal/db`.

---

## 8. Wallet‑RPC integration (view‑only)

Monalias v1 should **only** interact with **view‑only** wallets.

### 8.1 wallet-rpc config

Recommended docker command (simplified):

```bash
monero-wallet-rpc \
  --rpc-bind-ip=0.0.0.0 \
  --rpc-bind-port=18083 \
  --wallet-dir=/wallets \
  --daemon-address=<monerod_host:port> \
  --disable-rpc-login
```

In v1, Monalias does not need to talk to `monerod` directly; `wallet-rpc` is used for subaddress derivation only.

Environment variables:

- `MONALIAS_WALLET_RPC_URL` – e.g. `http://wallet-rpc:18083/json_rpc`
- `MONALIAS_WALLET_RPC_USER` (optional)
- `MONALIAS_WALLET_RPC_PASSWORD` (optional)

### 8.2 View-only wallets

Accounts can optionally reference a view‑only wallet:

- Admin is responsible for creating/importing the view‑only wallet externally.
- `wallet_name` in `accounts` points to the wallet file name in `wallet-dir`.

For `DYNAMIC_SUBADDRESS` aliases:

- The backend uses `wallet_name` and wallet-rpc to derive subaddresses.
- No spend/seed keys are ever stored or used in the Monalias codebase.

### 8.3 Dynamic alias flow (v1 suggestion)

On alias creation (`mode = DYNAMIC_SUBADDRESS`):

1. Use `wallet_name` to `open_wallet` via RPC.
2. Call `create_address` (or `get_address` with the next index) to get a subaddress and index.
3. Store the index in `next_subaddr_idx` and/or cache the address.

On resolve:

1. Lookup alias by `full_acct`.
2. If `mode = STATIC_ADDRESS` ⇒ return `static_address`.
3. If `mode = DYNAMIC_SUBADDRESS`:
   - Option A (simple v1): Always use stored index → one subaddress per alias.
   - Option B (optional enhancement): Use `next_subaddr_idx` and increment → per‑resolve fresh subaddress.
4. Use `get_address` if you need to re-derive the text form from index.
5. Sign and return as per protocol.

For v1, Option A is acceptable; store the subaddress at creation and reuse it per alias.

---

## 9. Identity watchdog

The server should periodically (e.g. every 15 minutes) validate that its **public metadata** matches its own configuration.

### 9.1 Process

1. Fetch `https://<MONALIAS_DOMAIN>/.well-known/monalias`.
2. Parse JSON and check:
   - `homeserver == MONALIAS_PUBLIC_BASE_URL`
   - At least one key object matches:
     - `kid == signing_key_id`
     - `public_key == signing_pubkey`

3. On failure to fetch (network error, timeout):
   - Count as a soft failure.
   - If repeated, set status `DEGRADED` with a reason (e.g. `"well_known_unreachable"`).

4. On mismatch (wrong homeserver or key):
   - Set status `LOCKED` with reason `"identity_mismatch"`.

Update `instance_config.status`, `status_reason`, `last_identity_check_at` accordingly.

### 9.2 Effects

- When `status = LOCKED`:
  - `/_monalias/resolve` should return `503` with `"instance_locked"`.
- When `status = DEGRADED`:
  - System may continue serving, but admin should be notified via admin UI/GraphQL.

Admin GraphQL (below) must provide ways to:

- Inspect `instanceInfo`
- Manually `lockInstance(reason)`
- Attempt `unlockInstance` (only succeed if `.well-known/monalias` is now correct)

---

## 10. Admin API (GraphQL, private)

Private GraphQL endpoint:

- Path: `/graphql`
- Bind: `127.0.0.1:8080` by default
- Should be protected by some simple auth:
  - HTTP basic auth with credentials from env/secrets
  - Or a fixed header like `X-Admin-Secret`

### 10.1 Schema outline (for gqlgen)

`schema.graphqls` (rough outline, Codex can refine):

```graphql
scalar DateTime

enum AliasMode {
  STATIC_ADDRESS
  DYNAMIC_SUBADDRESS
}

enum InstanceStatus {
  OK
  DEGRADED
  LOCKED
}

type InstanceInfo {
  domain: String!
  homeserver: String!
  signingKeyId: String!
  signingPubkey: String!
  status: InstanceStatus!
  statusReason: String
  lastIdentityCheckAt: DateTime
}

type Account {
  id: ID!
  handle: String!        # "bob$example.com"
  walletName: String
  createdAt: DateTime!
  aliases: [Alias!]!
}

type Alias {
  id: ID!
  fullAcct: String!      # "bob+rent$example.com"
  aliasLabel: String!
  mode: AliasMode!
  staticAddress: String
  nextSubaddrIdx: Int
  createdAt: DateTime!
  updatedAt: DateTime!
}

type Query {
  instanceInfo: InstanceInfo!
  accounts: [Account!]!
  account(id: ID!): Account
}

type Mutation {
  setInstanceConfig(domain: String!, homeserver: String!): InstanceInfo!

  createAccount(handle: String!, walletName: String): Account!
  deleteAccount(id: ID!): Boolean!

  createAlias(accountId: ID!, aliasLabel: String!, mode: AliasMode!): Alias!
  setAliasStaticAddress(aliasId: ID!, address: String!): Alias!
  setAliasMode(aliasId: ID!, mode: AliasMode!): Alias!
  setAliasNextIndex(aliasId: ID!, nextSubaddrIdx: Int!): Alias!

  lockInstance(reason: String!): InstanceInfo!
  unlockInstance: InstanceInfo!
  runIdentityCheck: InstanceInfo!
}
```

Use `gqlgen` to generate Go types/resolvers for this schema (output e.g. `internal/graphql`).

Resolvers should talk to the `internal/db` package (sqlc‑generated code) and any wallet‑RPC wrapper (`internal/monero`).

---

## 11. Admin UI (Flutter web, embedded in Go)

### 11.1 Flutter app

- Flutter app is purely an **admin UI**:
  - talks to `/graphql` on the admin listener
  - allows configuring instance, accounts, aliases
  - shows status, etc.

Target: **Flutter Web**.

Basic build command:

```bash
flutter build web --release
```

Outputs to `build/web` by default. The build artifacts must be copied into the Go project (or into the build image) under a known directory, e.g. `ui/dist`.

### 11.2 Embedding in Go binary

In the Go backend, create a package, e.g. `internal/ui`:

```go
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var embeddedFS embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
```

Where `dist` directory is populated by the Flutter build pipeline (e.g. copy `build/web` → `internal/ui/dist` during Docker build).

Public or admin router then mounts this handler on a path, e.g.:

```go
mux.Handle("/", ui.Handler())
```

For v1 we only need the admin UI on the private port (localhost:8080). It can consume the same `/graphql` endpoint that other admin tools would.

### 11.3 Build pipeline (Docker multi‑stage)

The `Dockerfile` should:

1. Use a Flutter image (or install Flutter) to build the web UI.
2. Copy the built web assets into the Go build context.
3. Build the Go binary with `embed.FS` picking up the web assets.

Pseudo‑Dockerfile flow:

```dockerfile
# === Flutter build stage ===
FROM cirrusci/flutter:stable AS flutter-build

WORKDIR /app
COPY ui/ ./
RUN flutter build web --release

# === Go build stage ===
FROM golang:1.22-alpine AS go-build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Copy Go source
COPY . .

# Copy Flutter web output into internal/ui/dist
COPY --from=flutter-build /app/build/web ./internal/ui/dist

RUN CGO_ENABLED=0 GOOS=linux go build -o monalias ./cmd/monalias

# === Runtime stage ===
FROM alpine:3.19

RUN adduser -D -H -u 10001 monalias
WORKDIR /app

COPY --from=go-build /app/monalias /app/monalias

RUN mkdir -p /data && chown monalias:monalias /data

USER monalias

ENV MONALIAS_DB_PATH=/data/monalias.db

EXPOSE 80    # public
EXPOSE 8080  # admin (localhost / VPN only)

ENTRYPOINT ["/app/monalias"]
```

Codex should adjust paths to match the actual repo layout it generates, but this is the general shape.

---

## 12. Configuration (.env + env vars + secrets)

### 12.1 Core env vars

These should be loadable from a `.env` file or environment directly:

```env
MONALIAS_DOMAIN=example.com
MONALIAS_PUBLIC_BASE_URL=https://monalias.example.com

MONALIAS_DB_PATH=/data/monalias.db

MONALIAS_RATE_IP_RPS=1.0
MONALIAS_RATE_IP_BURST=10

MONALIAS_CATCHALL_ADDRESS=   # optional, usually empty

MONALIAS_WALLET_RPC_URL=http://wallet-rpc:18083/json_rpc
MONALIAS_WALLET_RPC_USER=    # optional
MONALIAS_WALLET_RPC_PASSWORD= # optional
```

Signing key:

- `MONALIAS_SIGNING_KEY_FILE=/run/secrets/monalias_signing_key` (recommended)
- Or a direct path on disk, if not using secrets.

Admin auth (simple v1 approach):

- `MONALIAS_ADMIN_USER=admin`
- `MONALIAS_ADMIN_PASSWORD=some-strong-password`

### 12.2 Docker secrets

Recommended secrets:

- `monalias_signing_key` – Ed25519 private key for signing resolve responses.
- `wallet_rpc_password` – if wallet‑RPC is password‑protected.

Example `docker-compose.yml` fragment:

```yaml
secrets:
  monalias_signing_key:
    file: ./secrets/monalias_signing_key
  wallet_rpc_password:
    file: ./secrets/wallet_rpc_password
```

In the container:

- If `/run/secrets/monalias_signing_key` exists, read key from there.
- Else look at `MONALIAS_SIGNING_KEY_FILE` path.

Same pattern for `wallet_rpc_password` vs `MONALIAS_WALLET_RPC_PASSWORD`.

---

## 13. docker-compose.yml (example)

```yaml
version: "3.8"

services:
  wallet-rpc:
    image: monero-project/monero:latest
    command: >
      monero-wallet-rpc
      --rpc-bind-ip=0.0.0.0
      --rpc-bind-port=18083
      --wallet-dir=/wallets
      --disable-rpc-login
      --trusted-daemon
    volumes:
      - wallets:/wallets
    restart: unless-stopped

  monalias:
    build: .
    env_file:
      - .env
    environment:
      MONALIAS_WALLET_RPC_URL: "http://wallet-rpc:18083/json_rpc"
      MONALIAS_SIGNING_KEY_FILE: "/run/secrets/monalias_signing_key"
    secrets:
      - monalias_signing_key
    volumes:
      - monalias_data:/data
    ports:
      - "80:80"        # public resolver (and maybe Flutter UI if exposed)
      # DO NOT expose 8080 publicly; use SSH/VPN to reach admin GraphQL.
    restart: unless-stopped

secrets:
  monalias_signing_key:
    file: ./secrets/monalias_signing_key

volumes:
  monalias_data:
  wallets:
```

Instructions for newbies:

1. Create `.env` with at least:

   ```env
   MONALIAS_DOMAIN=example.com
   MONALIAS_PUBLIC_BASE_URL=https://monalias.example.com
   ```

2. Generate an Ed25519 keypair; save **private key** to `./secrets/monalias_signing_key`.
3. Run `docker compose up --build`.
4. Point your DNS:
   - `example.com` → your server
   - Add `.well-known/monalias` path (served by Monalias).
5. Do **not** expose port 8080 directly; reach admin via SSH tunnel:

   ```bash
   ssh -L 8080:127.0.0.1:8080 user@server
   ```

---

## 14. Project layout (suggested)

For Codex, suggested Go project layout:

```text
cmd/
  monalias/
    main.go          # wiring, HTTP servers, config

internal/
  config/
    config.go        # load env/.env, secrets
  db/
    schema.sql
    queries.sql
    db.go            # sqlc generated
  monero/
    wallet_rpc.go    # thin client for monero-wallet-rpc
  http/
    public.go        # public router (.well-known, resolve, healthz)
    admin.go         # admin router (/graphql)
    rate_limit.go    # middleware
  graphql/
    schema.graphqls
    resolvers.go     # gqlgen generated + custom
  ui/
    ui.go            # embed.FS handler
  identity/
    watchdog.go      # identity check logic

ui/
  # Flutter app source
  lib/
  pubspec.yaml
```

Codex can adjust, but the key points are:

- Separate public vs admin HTTP.
- Use `embed.FS` for Flutter web UI assets.
- Keep Monero RPC logic in its own small package.
- Use `sqlc` and `gqlgen` for DB and GraphQL plumbing.

---

This spec should be sufficient for Codex to generate:

- A Go backend with:
  - SQLite + sqlc
  - GraphQL admin API with gqlgen
  - Ed25519 signing of resolve responses
  - Rate limiting
  - Optional view‑only wallet integration
- A Flutter web admin UI bundled into the Go binary via `embed.FS`
- A Dockerfile + docker‑compose stack using `.env` and Docker secrets.
