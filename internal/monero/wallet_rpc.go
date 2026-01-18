package monero

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type WalletRPC struct {
	url      string
	user     string
	password string
	client   *http.Client
}

func NewWalletRPC(url, user, password string) *WalletRPC {
	return &WalletRPC{
		url:      url,
		user:     user,
		password: password,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *WalletRPC) Enabled() bool {
	return w != nil && w.url != ""
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (w *WalletRPC) call(ctx context.Context, method string, params interface{}, out interface{}) error {
	if w.url == "" {
		return errors.New("wallet rpc not configured")
	}

	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: "0", Method: method, Params: params})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.user != "" || w.password != "" {
		req.SetBasicAuth(w.user, w.password)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var parsed rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	if parsed.Error != nil {
		return fmt.Errorf("wallet rpc error %d: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return err
		}
	}
	return nil
}

func (w *WalletRPC) OpenWallet(ctx context.Context, name string) error {
	params := map[string]interface{}{"filename": name}
	return w.call(ctx, "open_wallet", params, nil)
}

type createAddressResult struct {
	Address      string `json:"address"`
	AddressIndex int64  `json:"address_index"`
}

func (w *WalletRPC) CreateAddress(ctx context.Context, label string) (string, int64, error) {
	params := map[string]interface{}{
		"account_index": 0,
	}
	if label != "" {
		params["label"] = label
	}
	var res createAddressResult
	if err := w.call(ctx, "create_address", params, &res); err != nil {
		return "", 0, err
	}
	return res.Address, res.AddressIndex, nil
}

type getAddressResult struct {
	Addresses []struct {
		Address      string `json:"address"`
		AddressIndex int64  `json:"address_index"`
	} `json:"addresses"`
}

func (w *WalletRPC) GetAddress(ctx context.Context, index int64) (string, error) {
	params := map[string]interface{}{
		"account_index": 0,
		"address_index": []int64{index},
	}
	var res getAddressResult
	if err := w.call(ctx, "get_address", params, &res); err != nil {
		return "", err
	}
	if len(res.Addresses) == 0 {
		return "", errors.New("address not found")
	}
	return res.Addresses[0].Address, nil
}
