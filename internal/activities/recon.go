package activities

import (
	"context"
	"encoding/json"

	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

// ReconReport is the outcome of one reconciliation run.
type ReconReport struct {
	ReportID   int64    `json:"report_id"`
	Matched    int      `json:"matched"`
	Missing    int      `json:"missing"`
	Mismatched int      `json:"mismatched"`
	Details    []string `json:"details"`
}

// Reconcile compares SUCCEEDED payouts with the bank's settled list and stores a report.
//
//	matched    = succeeded in the ledger and settled at the bank for the same amount
//	missing    = succeeded in the ledger but absent at the bank
//	mismatched = amount differs, or settled at the bank with no SUCCEEDED payout
func (a *Activities) Reconcile(ctx context.Context) (ReconReport, error) {
	succeeded, err := a.Q.ListPayoutsByStatus(ctx, OutcomeSucceeded)
	if err != nil {
		return ReconReport{}, err
	}
	settled, err := a.Mocks.Settled(ctx)
	if err != nil {
		return ReconReport{}, err
	}
	bank := make(map[string]SettledTransfer, len(settled))
	for _, s := range settled {
		bank[s.Reference] = s
	}
	rep := ReconReport{Details: []string{}}
	for _, p := range succeeded {
		s, ok := bank[p.ID]
		switch {
		case !ok:
			rep.Missing++
			rep.Details = append(rep.Details, "missing at bank: "+p.ID)
		case s.AmountMinor != p.AmountMinor:
			rep.Mismatched++
			rep.Details = append(rep.Details, "amount mismatch: "+p.ID)
		default:
			rep.Matched++
		}
		delete(bank, p.ID)
	}
	for ref := range bank {
		rep.Mismatched++
		rep.Details = append(rep.Details, "settled at bank without a succeeded payout: "+ref)
	}
	details, _ := json.Marshal(map[string]any{"details": rep.Details})
	row, err := a.Q.InsertReconReport(ctx, store.InsertReconReportParams{
		Matched: int32(rep.Matched), Missing: int32(rep.Missing), Mismatched: int32(rep.Mismatched), Details: details,
	})
	if err != nil {
		return ReconReport{}, err
	}
	rep.ReportID = row.ID
	a.Logger.Info("reconciliation", "matched", rep.Matched, "missing", rep.Missing, "mismatched", rep.Mismatched)
	return rep, nil
}
