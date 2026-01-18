-- name: GetInstanceConfig :one
SELECT * FROM instance_config WHERE id = 1;

-- name: UpsertInstanceConfig :one
INSERT INTO instance_config (id, domain, homeserver, signing_key_id, signing_pubkey, status, status_reason, last_identity_check_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  domain=excluded.domain,
  homeserver=excluded.homeserver,
  signing_key_id=excluded.signing_key_id,
  signing_pubkey=excluded.signing_pubkey,
  status=excluded.status,
  status_reason=excluded.status_reason,
  last_identity_check_at=excluded.last_identity_check_at
RETURNING *;

-- name: UpdateInstanceStatus :one
UPDATE instance_config
SET status = ?, status_reason = ?, last_identity_check_at = ?
WHERE id = 1
RETURNING *;

-- name: ListAccounts :many
SELECT * FROM accounts ORDER BY created_at;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = ?;

-- name: GetAccountByHandle :one
SELECT * FROM accounts WHERE handle = ?;

-- name: CreateAccount :one
INSERT INTO accounts (handle, wallet_name) VALUES (?, ?) RETURNING *;

-- name: DeleteAccount :exec
DELETE FROM accounts WHERE id = ?;

-- name: ListAliasesForAccount :many
SELECT * FROM aliases WHERE account_id = ? ORDER BY created_at;

-- name: GetAliasByFullAcct :one
SELECT * FROM aliases WHERE full_acct = ?;

-- name: GetAliasByID :one
SELECT * FROM aliases WHERE id = ?;

-- name: CreateAlias :one
INSERT INTO aliases (account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateAliasStaticAddress :one
UPDATE aliases SET static_address = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING *;

-- name: UpdateAliasMode :one
UPDATE aliases SET mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING *;

-- name: UpdateAliasNextIndex :one
UPDATE aliases SET next_subaddr_idx = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING *;
