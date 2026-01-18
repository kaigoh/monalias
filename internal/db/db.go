package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

type InstanceConfig struct {
	ID                  int64
	Domain              string
	Homeserver          string
	SigningKeyID        string
	SigningPubkey       string
	Status              string
	StatusReason        sql.NullString
	LastIdentityCheckAt sql.NullTime
}

type Account struct {
	ID         int64
	Handle     string
	WalletName sql.NullString
	CreatedAt  time.Time
}

type Alias struct {
	ID             int64
	AccountID      int64
	FullAcct       string
	AliasLabel     string
	Mode           string
	StaticAddress  sql.NullString
	NextSubaddrIdx sql.NullInt64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return nil, err
	}
	return &DB{sql: db}, nil
}

func (d *DB) Close() error {
	return d.sql.Close()
}

func (d *DB) InitSchema(ctx context.Context, schema string) error {
	if schema == "" {
		return errors.New("schema is empty")
	}
	_, err := d.sql.ExecContext(ctx, schema)
	return err
}

func (d *DB) GetInstanceConfig(ctx context.Context) (InstanceConfig, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, domain, homeserver, signing_key_id, signing_pubkey, status, status_reason, last_identity_check_at FROM instance_config WHERE id = 1`)
	var cfg InstanceConfig
	if err := row.Scan(&cfg.ID, &cfg.Domain, &cfg.Homeserver, &cfg.SigningKeyID, &cfg.SigningPubkey, &cfg.Status, &cfg.StatusReason, &cfg.LastIdentityCheckAt); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (d *DB) UpsertInstanceConfig(ctx context.Context, domain, homeserver, keyID, pubkey, status string, statusReason sql.NullString, lastCheck sql.NullTime) (InstanceConfig, error) {
	row := d.sql.QueryRowContext(ctx, `INSERT INTO instance_config (id, domain, homeserver, signing_key_id, signing_pubkey, status, status_reason, last_identity_check_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  domain=excluded.domain,
  homeserver=excluded.homeserver,
  signing_key_id=excluded.signing_key_id,
  signing_pubkey=excluded.signing_pubkey,
  status=excluded.status,
  status_reason=excluded.status_reason,
  last_identity_check_at=excluded.last_identity_check_at
RETURNING id, domain, homeserver, signing_key_id, signing_pubkey, status, status_reason, last_identity_check_at`,
		domain, homeserver, keyID, pubkey, status, statusReason, lastCheck,
	)
	var cfg InstanceConfig
	if err := row.Scan(&cfg.ID, &cfg.Domain, &cfg.Homeserver, &cfg.SigningKeyID, &cfg.SigningPubkey, &cfg.Status, &cfg.StatusReason, &cfg.LastIdentityCheckAt); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (d *DB) UpdateInstanceStatus(ctx context.Context, status string, reason sql.NullString, lastCheck sql.NullTime) (InstanceConfig, error) {
	row := d.sql.QueryRowContext(ctx, `UPDATE instance_config SET status = ?, status_reason = ?, last_identity_check_at = ? WHERE id = 1 RETURNING id, domain, homeserver, signing_key_id, signing_pubkey, status, status_reason, last_identity_check_at`,
		status, reason, lastCheck,
	)
	var cfg InstanceConfig
	if err := row.Scan(&cfg.ID, &cfg.Domain, &cfg.Homeserver, &cfg.SigningKeyID, &cfg.SigningPubkey, &cfg.Status, &cfg.StatusReason, &cfg.LastIdentityCheckAt); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (d *DB) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, handle, wallet_name, created_at FROM accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Handle, &a.WalletName, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) GetAccount(ctx context.Context, id int64) (Account, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, handle, wallet_name, created_at FROM accounts WHERE id = ?`, id)
	var a Account
	if err := row.Scan(&a.ID, &a.Handle, &a.WalletName, &a.CreatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) GetAccountByHandle(ctx context.Context, handle string) (Account, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, handle, wallet_name, created_at FROM accounts WHERE handle = ?`, handle)
	var a Account
	if err := row.Scan(&a.ID, &a.Handle, &a.WalletName, &a.CreatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) CreateAccount(ctx context.Context, handle string, walletName sql.NullString) (Account, error) {
	row := d.sql.QueryRowContext(ctx, `INSERT INTO accounts (handle, wallet_name) VALUES (?, ?) RETURNING id, handle, wallet_name, created_at`, handle, walletName)
	var a Account
	if err := row.Scan(&a.ID, &a.Handle, &a.WalletName, &a.CreatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) DeleteAccount(ctx context.Context, id int64) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	return err
}

func (d *DB) ListAliasesForAccount(ctx context.Context, accountID int64) ([]Alias, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at FROM aliases WHERE account_id = ? ORDER BY created_at`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Alias
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) GetAliasByFullAcct(ctx context.Context, fullAcct string) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at FROM aliases WHERE full_acct = ?`, fullAcct)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) GetAliasByID(ctx context.Context, id int64) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at FROM aliases WHERE id = ?`, id)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) CreateAlias(ctx context.Context, accountID int64, fullAcct, aliasLabel, mode string, staticAddress sql.NullString, nextIdx sql.NullInt64) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `INSERT INTO aliases (account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx)
VALUES (?, ?, ?, ?, ?, ?) RETURNING id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at`,
		accountID, fullAcct, aliasLabel, mode, staticAddress, nextIdx,
	)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) UpdateAliasStaticAddress(ctx context.Context, id int64, address sql.NullString) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `UPDATE aliases SET static_address = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at`,
		address, id,
	)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) UpdateAliasMode(ctx context.Context, id int64, mode string) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `UPDATE aliases SET mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at`,
		mode, id,
	)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) UpdateAliasNextIndex(ctx context.Context, id int64, nextIdx sql.NullInt64) (Alias, error) {
	row := d.sql.QueryRowContext(ctx, `UPDATE aliases SET next_subaddr_idx = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING id, account_id, full_acct, alias_label, mode, static_address, next_subaddr_idx, created_at, updated_at`,
		nextIdx, id,
	)
	var a Alias
	if err := row.Scan(&a.ID, &a.AccountID, &a.FullAcct, &a.AliasLabel, &a.Mode, &a.StaticAddress, &a.NextSubaddrIdx, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, err
	}
	return a, nil
}

func (d *DB) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed: %v (orig %w)", rbErr, err)
		}
		return err
	}
	return tx.Commit()
}

func (d *DB) SQL() *sql.DB {
	return d.sql
}
