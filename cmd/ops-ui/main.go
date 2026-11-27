// Command ops-ui is the operations dashboard (html/template + htmx).
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"

	payoutv1 "github.com/mini4141/payout-saga-orchestrator/gen/payout/v1"
	"github.com/mini4141/payout-saga-orchestrator/gen/payout/v1/payoutv1connect"
	"github.com/mini4141/payout-saga-orchestrator/internal/ledger"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
	"github.com/mini4141/payout-saga-orchestrator/internal/temporalx"
	"github.com/mini4141/payout-saga-orchestrator/workflows"
)

//go:embed templates/*.html static/*
var assets embed.FS

type app struct {
	pool     *pgxpool.Pool
	q        *store.Queries
	tc       client.Client
	rpc      payoutv1connect.PayoutServiceClient
	token    string
	mocksURL string
	temporal string // Temporal UI base URL, for deep links
	tpl      *template.Template
}

func main() {
	log := obs.SetupLogger("ops-ui")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, obs.Env("DATABASE_URL", ""))
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	tc, err := temporalx.Dial(ctx, obs.Env("TEMPORAL_ADDRESS", "temporal:7233"))
	if err != nil {
		log.Error("temporal", "err", err)
		os.Exit(1)
	}
	defer tc.Close()

	a := &app{
		pool: pool, q: store.New(pool), tc: tc, token: os.Getenv("API_TOKEN"),
		rpc:      payoutv1connect.NewPayoutServiceClient(http.DefaultClient, obs.Env("API_URL", "http://api:8080")),
		mocksURL: obs.Env("MOCKS_URL", "http://mocks:8090"),
		temporal: obs.Env("TEMPORAL_UI_URL", "http://localhost:8233"),
	}
	a.tpl = template.Must(template.New("").Funcs(template.FuncMap{
		"money":  money,
		"ts":     func(t time.Time) string { return t.Local().Format("15:04:05.000") },
		"lower":  strings.ToLower,
		"pct":    func(v float64) int { return int(v*100 + 0.5) },
		"barPct": barPct,
	}).ParseFS(assets, "templates/*.html"))

	mux := obs.Mux()
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /{$}", a.page("payouts.html", a.payoutsData))
	mux.HandleFunc("GET /fragments/payouts", a.page("payouts_table", a.payoutsData))
	mux.HandleFunc("GET /payouts/{id}", a.page("payout.html", a.payoutData))
	mux.HandleFunc("GET /fragments/payouts/{id}", a.page("payout_body", a.payoutData))
	mux.HandleFunc("GET /inject", a.page("inject.html", a.injectData))
	mux.HandleFunc("GET /recon", a.page("recon.html", a.reconData))
	mux.HandleFunc("GET /invariants", a.page("invariants.html", a.invariantsData))
	if os.Getenv("DEBUG_SQL") == "true" { // cloud drills: RDS is private, so SELECTs go through the ops UI
		mux.HandleFunc("GET /debug/sql", a.debugSQL)
	}
	mux.HandleFunc("POST /demo/payouts", a.createDemo)
	mux.HandleFunc("POST /inject/{service}", a.setInject)
	mux.HandleFunc("POST /inject-reset", a.resetInject)
	mux.HandleFunc("POST /recon/run", a.runRecon)
	mux.HandleFunc("POST /recon/seed-mismatch", a.seedMismatch)

	addr := obs.Env("OPS_UI_ADDR", ":8081")
	log.Info("ops-ui listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error("ops-ui stopped", "err", err)
		os.Exit(1)
	}
}

func money(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

func barPct(ms, max int64) int {
	if max <= 0 {
		return 0
	}
	p := int(ms * 100 / max)
	if p < 2 && ms > 0 {
		p = 2
	}
	return p
}

type dataFn func(r *http.Request) (any, error)

func (a *app) page(name string, fn dataFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fn(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		if err := a.tpl.ExecuteTemplate(&buf, name, data); err != nil {
			slog.Error("render", "template", name, "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

func (a *app) payoutsData(r *http.Request) (any, error) {
	rows, err := a.q.ListPayouts(r.Context(), 100)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, p := range rows {
		counts[p.Status]++
	}
	return map[string]any{"Nav": "payouts", "Payouts": rows, "Counts": counts}, nil
}

type timelineRow struct {
	store.StepAttempt
	Label string
	Bar   int
}

func (a *app) payoutData(r *http.Request) (any, error) {
	id := r.PathValue("id")
	p, err := a.q.GetPayout(r.Context(), id)
	if err != nil {
		return nil, fmt.Errorf("payout %s: %w", id, err)
	}
	atts, err := a.q.ListAttempts(r.Context(), id)
	if err != nil {
		return nil, err
	}
	var max int64
	lastIdx := map[string]int{}
	for i, t := range atts {
		if t.DurationMs > max {
			max = t.DurationMs
		}
		lastIdx[t.Step] = i
	}
	rows := make([]timelineRow, len(atts))
	compensations := []string{}
	for i, t := range atts {
		label := t.Outcome
		if t.Outcome == "started" && (i < lastIdx[t.Step] || p.Status != "PROCESSING" && p.Status != "PENDING") {
			label = "lost (worker died)"
		}
		rows[i] = timelineRow{StepAttempt: t, Label: label, Bar: barPct(t.DurationMs, max)}
		if (t.Step == "Release" || t.Step == "CancelFX") && t.Outcome == "ok" {
			compensations = append(compensations, t.Step)
		}
	}
	results, _ := a.q.ListStepResults(r.Context(), id)
	type res struct{ Step, JSON string }
	var rs []res
	for _, s := range results {
		rs = append(rs, res{s.Step, string(s.Result)})
	}
	return map[string]any{
		"Nav": "payouts", "P": p, "Timeline": rows, "Results": rs, "Compensations": compensations,
		"TemporalURL": fmt.Sprintf("%s/namespaces/default/workflows/%s", a.temporal, workflows.WorkflowID(id)),
	}, nil
}

type mockCfg struct {
	FailRate    float64 `json:"fail_rate"`
	LatencyMS   int     `json:"latency_ms"`
	ErrorNext   int     `json:"error_next"`
	RejectNext  int     `json:"reject_next"`
	TimeoutNext int     `json:"timeout_next"`
}

func (a *app) injectData(r *http.Request) (any, error) {
	resp, err := http.Get(a.mocksURL + "/admin/state")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var st struct {
		Config map[string]mockCfg `json:"config"`
		Calls  map[string]int     `json:"calls"`
		Replay map[string]int     `json:"idempotent_replays"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return nil, err
	}
	return map[string]any{"Nav": "inject", "Services": []string{"fx", "compliance", "bank"}, "Config": st.Config, "Calls": st.Calls, "Replays": st.Replay}, nil
}

func (a *app) setInject(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	atoi := func(k string) int { v, _ := strconv.Atoi(r.FormValue(k)); return v }
	failPct, _ := strconv.ParseFloat(r.FormValue("fail_rate"), 64)
	body, _ := json.Marshal(mockCfg{
		FailRate: failPct / 100, LatencyMS: atoi("latency_ms"), ErrorNext: atoi("error_next"),
		RejectNext: atoi("reject_next"), TimeoutNext: atoi("timeout_next"),
	})
	resp, err := http.Post(a.mocksURL+"/admin/"+r.PathValue("service")+"/config", "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_ = resp.Body.Close()
	http.Redirect(w, r, "/inject", http.StatusSeeOther)
}

func (a *app) resetInject(w http.ResponseWriter, r *http.Request) {
	if resp, err := http.Post(a.mocksURL+"/admin/reset", "application/json", nil); err == nil {
		_ = resp.Body.Close()
	}
	http.Redirect(w, r, "/inject", http.StatusSeeOther)
}

func (a *app) reconData(r *http.Request) (any, error) {
	rows, err := a.q.ListReconReports(r.Context(), 20)
	if err != nil {
		return nil, err
	}
	type rep struct {
		store.ReconReport
		Lines []string
	}
	out := make([]rep, len(rows))
	for i, x := range rows {
		var d struct {
			Details []string `json:"details"`
		}
		_ = json.Unmarshal(x.Details, &d)
		out[i] = rep{x, d.Details}
	}
	return map[string]any{"Nav": "recon", "Reports": out, "Cron": workflows.ReconCronSchedule}, nil
}

func (a *app) runRecon(w http.ResponseWriter, r *http.Request) {
	_, err := a.tc.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{
		ID: fmt.Sprintf("reconciliation-manual-%d", time.Now().UnixNano()), TaskQueue: workflows.TaskQueue,
	}, workflows.ReconciliationWorkflow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	time.Sleep(1500 * time.Millisecond) // let the one-off run finish before the page reloads
	http.Redirect(w, r, "/recon", http.StatusSeeOther)
}

func (a *app) seedMismatch(w http.ResponseWriter, r *http.Request) {
	body := fmt.Sprintf(`{"reference":"po_orphan_%d","amount_minor":12345,"currency":"USD"}`, time.Now().Unix())
	resp, err := http.Post(a.mocksURL+"/admin/bank/seed-settled", "application/json", strings.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	http.Redirect(w, r, "/recon", http.StatusSeeOther)
}

func (a *app) invariantsData(r *http.Request) (any, error) {
	inv, err := ledger.CheckInvariants(r.Context(), a.q)
	if err != nil {
		return nil, err
	}
	ob, err := a.q.CountOutbox(r.Context())
	if err != nil {
		return nil, err
	}
	orphans, err := a.q.OrphanHolds(r.Context())
	if err != nil {
		return nil, err
	}
	all := true
	for _, i := range inv {
		all = all && i.OK
	}
	return map[string]any{"Nav": "invariants", "Rows": inv, "AllOK": all && len(orphans) == 0, "Outbox": ob, "Orphans": orphans}, nil
}

// createDemo creates n payouts through the Connect API (the same path as any client).
func (a *app) createDemo(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.FormValue("n"))
	if n <= 0 || n > 50 {
		n = 10
	}
	merchants := []string{"m_acme", "m_bluebird", "m_cobalt", "m_delta", "m_ember"}
	batch := time.Now().UnixNano()
	for i := 0; i < n; i++ {
		req := connect.NewRequest(&payoutv1.CreatePayoutRequest{
			MerchantId: merchants[i%len(merchants)], AmountMinor: int64(10_000 + 2_500*i), Currency: "AUD", TargetCurrency: "USD",
			BankAccount: "062-000-1234", IdempotencyKey: fmt.Sprintf("demo-%d-%d", batch, i),
		})
		if a.token != "" {
			req.Header().Set("Authorization", "Bearer "+a.token)
		}
		if _, err := a.rpc.CreatePayout(r.Context(), req); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// debugSQL runs one read-only SELECT and prints rows as '|'-separated lines (psql -At style).
// It exists so the failure drills can assert on database state in the cloud, where RDS is private.
// Enabled only with DEBUG_SQL=true, requires the API bearer token, and runs in a read-only transaction.
func (a *app) debugSQL(w http.ResponseWriter, r *http.Request) {
	if a.token == "" || r.Header.Get("Authorization") != "Bearer "+a.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if !strings.HasPrefix(strings.ToLower(q), "select") || strings.Contains(q, ";") {
		http.Error(w, "only a single SELECT is allowed", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			if v != nil {
				cells[i] = fmt.Sprint(v)
			}
		}
		b.WriteString(strings.Join(cells, "|") + "\n")
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}
