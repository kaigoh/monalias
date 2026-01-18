package graphql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	graph "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"

	"github.com/kaigoh/monalias/internal/config"
	"github.com/kaigoh/monalias/internal/db"
	"github.com/kaigoh/monalias/internal/identity"
	"github.com/kaigoh/monalias/internal/monero"
)

//go:embed schema.graphqls
var schemaFS embed.FS

func NewHandler(cfg config.Config, database *db.DB, wallet *monero.WalletRPC, watchdog *identity.Watchdog) (*relay.Handler, error) {
	schemaBytes, err := schemaFS.ReadFile("schema.graphqls")
	if err != nil {
		return nil, err
	}

	resolvers := &Resolver{
		cfg:      cfg,
		db:       database,
		wallet:   wallet,
		watchdog: watchdog,
	}
	schema := graph.MustParseSchema(string(schemaBytes), resolvers)
	return &relay.Handler{Schema: schema}, nil
}

type Resolver struct {
	cfg      config.Config
	db       *db.DB
	wallet   *monero.WalletRPC
	watchdog *identity.Watchdog
}

func (r *Resolver) InstanceInfo(ctx context.Context) (*InstanceInfoResolver, error) {
	cfg, err := r.db.GetInstanceConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &InstanceInfoResolver{cfg: cfg}, nil
}

func (r *Resolver) Accounts(ctx context.Context) ([]*AccountResolver, error) {
	accounts, err := r.db.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	resolvers := make([]*AccountResolver, 0, len(accounts))
	for _, account := range accounts {
		resolvers = append(resolvers, &AccountResolver{db: r.db, account: account})
	}
	return resolvers, nil
}

func (r *Resolver) Account(ctx context.Context, args struct{ ID graph.ID }) (*AccountResolver, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return nil, err
	}
	account, err := r.db.GetAccount(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &AccountResolver{db: r.db, account: account}, nil
}

func (r *Resolver) SetInstanceConfig(ctx context.Context, args struct {
	Domain     string
	Homeserver string
}) (*InstanceInfoResolver, error) {
	current, err := r.db.GetInstanceConfig(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	status := current.Status
	if status == "" {
		status = "OK"
	}
	keyID := current.SigningKeyID
	if keyID == "" {
		keyID = r.cfg.SigningKeyID
	}
	cfg, err := r.db.UpsertInstanceConfig(ctx, args.Domain, args.Homeserver, keyID, current.SigningPubkey, status, current.StatusReason, current.LastIdentityCheckAt)
	if err != nil {
		return nil, err
	}
	return &InstanceInfoResolver{cfg: cfg}, nil
}

func (r *Resolver) CreateAccount(ctx context.Context, args struct {
	Handle     string
	WalletName *string
}) (*AccountResolver, error) {
	if !acctMatchesDomain(args.Handle, r.cfg.Domain) {
		return nil, fmt.Errorf("handle must match domain %s", r.cfg.Domain)
	}
	wallet := sql.NullString{}
	if args.WalletName != nil && *args.WalletName != "" {
		wallet = sql.NullString{String: *args.WalletName, Valid: true}
	}
	account, err := r.db.CreateAccount(ctx, args.Handle, wallet)
	if err != nil {
		return nil, err
	}
	return &AccountResolver{db: r.db, account: account}, nil
}

func (r *Resolver) DeleteAccount(ctx context.Context, args struct{ ID graph.ID }) (bool, error) {
	id, err := parseID(args.ID)
	if err != nil {
		return false, err
	}
	if err := r.db.DeleteAccount(ctx, id); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Resolver) CreateAlias(ctx context.Context, args struct {
	AccountID  graph.ID
	AliasLabel string
	Mode       string
}) (*AliasResolver, error) {
	accountID, err := parseID(args.AccountID)
	if err != nil {
		return nil, err
	}
	account, err := r.db.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	fullAcct := buildFullAcct(account.Handle, args.AliasLabel)

	var staticAddress sql.NullString
	var nextIdx sql.NullInt64

	if args.Mode == "DYNAMIC_SUBADDRESS" {
		if r.wallet == nil || !r.wallet.Enabled() {
			return nil, errors.New("wallet rpc is not configured")
		}
		if !account.WalletName.Valid || account.WalletName.String == "" {
			return nil, errors.New("wallet name is required for dynamic alias")
		}
		if err := r.wallet.OpenWallet(ctx, account.WalletName.String); err != nil {
			return nil, err
		}
		addr, idx, err := r.wallet.CreateAddress(ctx, args.AliasLabel)
		if err != nil {
			return nil, err
		}
		staticAddress = sql.NullString{String: addr, Valid: true}
		nextIdx = sql.NullInt64{Int64: idx, Valid: true}
	}

	alias, err := r.db.CreateAlias(ctx, accountID, fullAcct, args.AliasLabel, args.Mode, staticAddress, nextIdx)
	if err != nil {
		return nil, err
	}
	return &AliasResolver{alias: alias}, nil
}

func (r *Resolver) SetAliasStaticAddress(ctx context.Context, args struct {
	AliasID graph.ID
	Address string
}) (*AliasResolver, error) {
	id, err := parseID(args.AliasID)
	if err != nil {
		return nil, err
	}
	alias, err := r.db.UpdateAliasStaticAddress(ctx, id, sql.NullString{String: args.Address, Valid: true})
	if err != nil {
		return nil, err
	}
	return &AliasResolver{alias: alias}, nil
}

func (r *Resolver) SetAliasMode(ctx context.Context, args struct {
	AliasID graph.ID
	Mode    string
}) (*AliasResolver, error) {
	id, err := parseID(args.AliasID)
	if err != nil {
		return nil, err
	}
	alias, err := r.db.UpdateAliasMode(ctx, id, args.Mode)
	if err != nil {
		return nil, err
	}
	if args.Mode == "DYNAMIC_SUBADDRESS" && !alias.NextSubaddrIdx.Valid {
		account, err := r.db.GetAccount(ctx, alias.AccountID)
		if err != nil {
			return nil, err
		}
		if r.wallet == nil || !r.wallet.Enabled() {
			return nil, errors.New("wallet rpc is not configured")
		}
		if !account.WalletName.Valid || account.WalletName.String == "" {
			return nil, errors.New("wallet name is required for dynamic alias")
		}
		if err := r.wallet.OpenWallet(ctx, account.WalletName.String); err != nil {
			return nil, err
		}
		addr, idx, err := r.wallet.CreateAddress(ctx, alias.AliasLabel)
		if err != nil {
			return nil, err
		}
		alias, err = r.db.UpdateAliasStaticAddress(ctx, id, sql.NullString{String: addr, Valid: true})
		if err != nil {
			return nil, err
		}
		alias, err = r.db.UpdateAliasNextIndex(ctx, id, sql.NullInt64{Int64: idx, Valid: true})
		if err != nil {
			return nil, err
		}
	}
	return &AliasResolver{alias: alias}, nil
}

func (r *Resolver) SetAliasNextIndex(ctx context.Context, args struct {
	AliasID        graph.ID
	NextSubaddrIdx int32
}) (*AliasResolver, error) {
	id, err := parseID(args.AliasID)
	if err != nil {
		return nil, err
	}
	alias, err := r.db.UpdateAliasNextIndex(ctx, id, sql.NullInt64{Int64: int64(args.NextSubaddrIdx), Valid: true})
	if err != nil {
		return nil, err
	}
	return &AliasResolver{alias: alias}, nil
}

func (r *Resolver) LockInstance(ctx context.Context, args struct{ Reason string }) (*InstanceInfoResolver, error) {
	cfg, err := r.db.UpdateInstanceStatus(ctx, "LOCKED", sql.NullString{String: args.Reason, Valid: true}, sql.NullTime{Time: time.Now().UTC(), Valid: true})
	if err != nil {
		return nil, err
	}
	return &InstanceInfoResolver{cfg: cfg}, nil
}

func (r *Resolver) UnlockInstance(ctx context.Context) (*InstanceInfoResolver, error) {
	cfg, err := r.watchdog.CheckOnce(ctx)
	if err != nil {
		return nil, err
	}
	return &InstanceInfoResolver{cfg: cfg}, nil
}

func (r *Resolver) RunIdentityCheck(ctx context.Context) (*InstanceInfoResolver, error) {
	cfg, err := r.watchdog.CheckOnce(ctx)
	if err != nil {
		return nil, err
	}
	return &InstanceInfoResolver{cfg: cfg}, nil
}

// --- Resolvers ---

type InstanceInfoResolver struct {
	cfg db.InstanceConfig
}

func (r *InstanceInfoResolver) Domain() string        { return r.cfg.Domain }
func (r *InstanceInfoResolver) Homeserver() string    { return r.cfg.Homeserver }
func (r *InstanceInfoResolver) SigningKeyId() string  { return r.cfg.SigningKeyID }
func (r *InstanceInfoResolver) SigningPubkey() string { return r.cfg.SigningPubkey }
func (r *InstanceInfoResolver) Status() string        { return r.cfg.Status }
func (r *InstanceInfoResolver) StatusReason() *string {
	if r.cfg.StatusReason.Valid {
		return &r.cfg.StatusReason.String
	}
	return nil
}
func (r *InstanceInfoResolver) LastIdentityCheckAt() *graph.Time {
	if r.cfg.LastIdentityCheckAt.Valid {
		return &graph.Time{Time: r.cfg.LastIdentityCheckAt.Time}
	}
	return nil
}

type AccountResolver struct {
	db      *db.DB
	account db.Account
}

func (r *AccountResolver) ID() graph.ID   { return graph.ID(fmt.Sprintf("%d", r.account.ID)) }
func (r *AccountResolver) Handle() string { return r.account.Handle }
func (r *AccountResolver) WalletName() *string {
	if r.account.WalletName.Valid {
		return &r.account.WalletName.String
	}
	return nil
}
func (r *AccountResolver) CreatedAt() graph.Time { return graph.Time{Time: r.account.CreatedAt} }
func (r *AccountResolver) Aliases(ctx context.Context) ([]*AliasResolver, error) {
	aliases, err := r.db.ListAliasesForAccount(ctx, r.account.ID)
	if err != nil {
		return nil, err
	}
	resolvers := make([]*AliasResolver, 0, len(aliases))
	for _, alias := range aliases {
		resolvers = append(resolvers, &AliasResolver{alias: alias})
	}
	return resolvers, nil
}

type AliasResolver struct {
	alias db.Alias
}

func (r *AliasResolver) ID() graph.ID       { return graph.ID(fmt.Sprintf("%d", r.alias.ID)) }
func (r *AliasResolver) FullAcct() string   { return r.alias.FullAcct }
func (r *AliasResolver) AliasLabel() string { return r.alias.AliasLabel }
func (r *AliasResolver) Mode() string       { return r.alias.Mode }
func (r *AliasResolver) StaticAddress() *string {
	if r.alias.StaticAddress.Valid {
		return &r.alias.StaticAddress.String
	}
	return nil
}
func (r *AliasResolver) NextSubaddrIdx() *int32 {
	if r.alias.NextSubaddrIdx.Valid {
		val := int32(r.alias.NextSubaddrIdx.Int64)
		return &val
	}
	return nil
}
func (r *AliasResolver) CreatedAt() graph.Time { return graph.Time{Time: r.alias.CreatedAt} }
func (r *AliasResolver) UpdatedAt() graph.Time { return graph.Time{Time: r.alias.UpdatedAt} }

// --- Helpers ---

func parseID(id graph.ID) (int64, error) {
	parsed, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id")
	}
	return parsed, nil
}

func acctMatchesDomain(acct, domain string) bool {
	parts := strings.Split(acct, "$")
	if len(parts) != 2 {
		return false
	}
	return strings.EqualFold(parts[1], domain)
}

func buildFullAcct(handle, label string) string {
	if label == "" || label == "default" {
		return handle
	}
	parts := strings.Split(handle, "$")
	if len(parts) != 2 {
		return handle
	}
	local := parts[0]
	domain := parts[1]
	return fmt.Sprintf("%s+%s$%s", local, label, domain)
}
