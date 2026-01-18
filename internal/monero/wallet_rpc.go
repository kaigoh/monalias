package monero

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"gitlab.com/moneropay/go-monero/walletrpc"
)

type WalletRPC struct {
	client *walletrpc.Client
}

func NewWalletRPC(url, user, password string) *WalletRPC {
	headers := map[string]string{}
	if user != "" || password != "" {
		token := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
		headers["Authorization"] = "Basic " + token
	}

	client := walletrpc.New(walletrpc.Config{
		Address:       url,
		CustomHeaders: headers,
		Client:        &http.Client{Timeout: 10 * time.Second},
	})

	return &WalletRPC{client: client}
}

func (w *WalletRPC) Enabled() bool {
	return w != nil && w.client != nil
}

func (w *WalletRPC) OpenWallet(ctx context.Context, name string) error {
	if w.client == nil {
		return errors.New("wallet rpc not configured")
	}
	return w.client.OpenWallet(ctx, &walletrpc.OpenWalletRequest{Filename: name})
}

func (w *WalletRPC) CreateAddress(ctx context.Context, label string) (string, int64, error) {
	if w.client == nil {
		return "", 0, errors.New("wallet rpc not configured")
	}
	resp, err := w.client.CreateAddress(ctx, &walletrpc.CreateAddressRequest{
		AccountIndex: 0,
		Label:        label,
	})
	if err != nil {
		return "", 0, err
	}
	return resp.Address, int64(resp.AddressIndex), nil
}

func (w *WalletRPC) GetAddress(ctx context.Context, index int64) (string, error) {
	if w.client == nil {
		return "", errors.New("wallet rpc not configured")
	}
	resp, err := w.client.GetAddress(ctx, &walletrpc.GetAddressRequest{
		AccountIndex: 0,
		AddressIndex: []uint64{uint64(index)},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Addresses) == 0 {
		return "", errors.New("address not found")
	}
	return resp.Addresses[0].Address, nil
}
