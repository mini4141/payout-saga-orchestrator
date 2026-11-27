package sfn_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type state struct {
	Type  string `json:"Type"`
	Next  string `json:"Next"`
	End   bool   `json:"End"`
	Retry []struct {
		ErrorEquals []string `json:"ErrorEquals"`
	} `json:"Retry"`
	Catch []struct {
		ErrorEquals []string `json:"ErrorEquals"`
		Next        string   `json:"Next"`
	} `json:"Catch"`
}

// Structural check of the ASL: every transition resolves, every state is reachable, the bank
// step catches BankRejected into the compensation branch. Phase 2 also runs
// `aws stepfunctions validate-state-machine-definition` on the same file.
func TestPayoutASLIsWellFormed(t *testing.T) {
	raw, err := os.ReadFile("payout.asl.json")
	if err != nil {
		t.Fatal(err)
	}
	var def struct {
		StartAt string           `json:"StartAt"`
		States  map[string]state `json:"States"`
	}
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "${", "arn:test:${")), &def); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	reached := map[string]bool{def.StartAt: true}
	var visit func(string)
	visit = func(n string) {
		s, ok := def.States[n]
		if !ok {
			t.Fatalf("transition to unknown state %q", n)
		}
		next := []string{}
		if s.Next != "" {
			next = append(next, s.Next)
		}
		for _, c := range s.Catch {
			next = append(next, c.Next)
		}
		if s.Next == "" && !s.End {
			t.Fatalf("state %s has neither Next nor End", n)
		}
		for _, x := range next {
			if !reached[x] {
				reached[x] = true
				visit(x)
			}
		}
	}
	visit(def.StartAt)
	for n := range def.States {
		if !reached[n] {
			t.Errorf("state %s is unreachable", n)
		}
	}

	bank := def.States["SendBankTransfer"]
	if len(bank.Retry) == 0 {
		t.Error("SendBankTransfer needs a Retry block")
	}
	found := false
	for _, c := range bank.Catch {
		if len(c.ErrorEquals) == 1 && c.ErrorEquals[0] == "BankRejected" && c.Next == "CompensateCancelFX" {
			found = true
		}
	}
	if !found {
		t.Error("SendBankTransfer must Catch BankRejected into CompensateCancelFX")
	}
	for _, r := range bank.Retry {
		for _, e := range r.ErrorEquals {
			if e == "BankRejected" || e == "States.ALL" {
				t.Errorf("bank Retry must not retry %s", e)
			}
		}
	}
}
