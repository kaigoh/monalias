# Monalias Lookup Spec (v0.1)

This document describes how third parties resolve a Monalias ID into a Monero address.

Monalias is DNS-style: `local_part$domain` with an optional `+alias` label.

Examples:

- `bob$example.com`
- `bob+rent$example.com`

The optional URI form is `xmr:local_part$domain` for QR codes and deep links. The server does not need to parse the `xmr:` prefix.

## 1. Well-known metadata

Each Monalias domain must serve a metadata document at:

```
GET https://<domain>/.well-known/monalias
```

Response:

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

- `homeserver`: base URL that exposes `/_monalias/resolve`.
- `version`: protocol version, `0.1`.
- `keys`: signing keys for resolve responses.

## 2. Resolve endpoint

Clients POST to the homeserver:

```
POST https://<homeserver>/_monalias/resolve
Content-Type: application/json
```

Request body:

```json
{
  "acct": "bob+rent$example.com",
  "network": "mainnet"
}
```

Fields:

- `acct`: Monalias ID, required.
- `network`: `mainnet` or `stagenet`, required.

Success response (200):

Headers:

```
X-Monalias-Key-Id: main-2026-01
X-Monalias-Sig: BASE64_SIGNATURE
```

Body:

```json
{
  "address": "89MoneroAddressOrSubaddress...",
  "network": "mainnet",
  "meta": {
    "display_name": "Bob",
    "alias": "rent",
    "resolved_kind": "NORMAL"
  },
  "expires_at": "2026-01-18T11:30:00Z"
}
```

Fields:

- `address`: Monero address or subaddress.
- `network`: echo of the request.
- `meta.display_name`: optional UI label.
- `meta.alias`: optional alias label (example: `rent`).
- `meta.resolved_kind`: `NORMAL` or `CATCH_ALL`.
- `expires_at`: optional ISO8601 UTC timestamp.

## 3. Signature verification

Resolve responses are signed with Ed25519. Clients verify the signature using the `public_key` that matches the `kid` from the well-known document.

Canonical string to sign (newlines are literal `\n`):

```
MONALIAS_RESOLVE
<acct>
<address>
<network>
<expires_at_or_empty>
<key_id>
```

Rules:

- `expires_at_or_empty` is the `expires_at` string or empty if unset.
- `key_id` is the `kid` used for signing.

Signature is base64-encoded in `X-Monalias-Sig`.

## 4. Errors

Unknown alias (no catch-all configured):

```
HTTP/1.1 404 Not Found
Content-Type: application/json
```

```json
{
  "error": "alias_not_found"
}
```

Rate limited:

```
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

Instance locked:

```
HTTP/1.1 503 Service Unavailable
Content-Type: application/json
```

```json
{
  "error": "instance_locked",
  "reason": "identity_mismatch"
}
```

## 5. Client lookup flow

1. Parse the Monalias ID and extract `domain`.
2. Fetch `https://<domain>/.well-known/monalias`.
3. POST `acct` and `network` to `<homeserver>/_monalias/resolve`.
4. Verify the signature using the matching `kid`.
5. Use the `address`.

Optional SRV fallback:

If the well-known lookup fails, clients may resolve:

```
_monalias._tcp.<domain>  IN  SRV  priority weight port target
```

and call the resolver on the SRV host and port.
