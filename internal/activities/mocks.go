package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

// Domain errors. Their Go type names (BankRejected, ComplianceRejected) are the error
// types matched by Temporal's NonRetryableErrorTypes and by Step Functions' Catch.
type (
	BankRejected       struct{ Msg string }
	ComplianceRejected struct{ Msg string }
)

func (e BankRejected) Error() string       { return e.Msg }
func (e ComplianceRejected) Error() string { return e.Msg }

// Mocks is the HTTP client for the FX, compliance and bank mock services.
type Mocks struct {
	BaseURL string
	HTTP    *http.Client
}

// NewMocks returns a client with a per-call timeout just under the bank step's 10 s StartToCloseTimeout.
func NewMocks(base string) *Mocks {
	return &Mocks{BaseURL: base, HTTP: &http.Client{Timeout: 9 * time.Second}}
}

func (m *Mocks) post(ctx context.Context, path, idemKey string, in any) (map[string]any, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: http %d: %s", path, resp.StatusCode, bytes.TrimSpace(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", path, err)
	}
	return out, nil
}

// Quote locks an FX quote.
func (m *Mocks) Quote(ctx context.Context, p store.Payout) (map[string]any, error) {
	return m.post(ctx, "/fx/quote", p.ID+":fx", map[string]any{
		"from": p.Currency, "to": p.TargetCurrency, "amount_minor": p.AmountMinor,
	})
}

// CancelQuote cancels an FX lock.
func (m *Mocks) CancelQuote(ctx context.Context, p store.Payout, quoteID string) (map[string]any, error) {
	return m.post(ctx, "/fx/cancel", p.ID+":fx-cancel", map[string]any{"quote_id": quoteID})
}

// Screen runs compliance screening and maps BLOCKED to a non-retryable error.
func (m *Mocks) Screen(ctx context.Context, p store.Payout) (map[string]any, error) {
	out, err := m.post(ctx, "/compliance/screen", p.ID+":screen", map[string]any{
		"payout_id": p.ID, "merchant_id": p.MerchantID, "amount_minor": p.AmountMinor,
	})
	if err != nil {
		return nil, err
	}
	if out["decision"] == "BLOCKED" {
		return nil, ComplianceRejected{Msg: "compliance blocked payout " + p.ID}
	}
	return out, nil
}

// Transfer sends the bank transfer and maps REJECTED to a non-retryable error.
func (m *Mocks) Transfer(ctx context.Context, p store.Payout, quoteID string) (map[string]any, error) {
	out, err := m.post(ctx, "/bank/transfer", p.ID+":bank", map[string]any{
		"reference": p.ID, "amount_minor": p.AmountMinor, "currency": p.TargetCurrency,
		"account": p.BankAccount, "fx_quote_id": quoteID,
	})
	if err != nil {
		return nil, err
	}
	if out["status"] == "REJECTED" {
		return nil, BankRejected{Msg: fmt.Sprintf("bank rejected payout %s: %v", p.ID, out["reason"])}
	}
	return out, nil
}

// Settled lists transfers settled at the bank (used by reconciliation).
func (m *Mocks) Settled(ctx context.Context) ([]SettledTransfer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.BaseURL+"/bank/settled", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bank settled: http %d", resp.StatusCode)
	}
	var out struct {
		Transfers []SettledTransfer `json:"transfers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Transfers, nil
}

// SettledTransfer is one row of the bank's settled list.
type SettledTransfer struct {
	Reference   string `json:"reference"`
	AmountMinor int64  `json:"amount_minor"`
	TransferID  string `json:"transfer_id"`
}
