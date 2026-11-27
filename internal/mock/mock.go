// Package mock implements the FX, compliance and bank-rail mock services. Each has a
// failure/latency injection admin API under /admin, and honours the Idempotency-Key header.
package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Config is the injection config of one mock service.
type Config struct {
	FailRate    float64 `json:"fail_rate"`    // probability of an HTTP 503 per call
	LatencyMS   int     `json:"latency_ms"`   // added latency per call
	ErrorNext   int     `json:"error_next"`   // next N calls return HTTP 503
	RejectNext  int     `json:"reject_next"`  // bank: next N transfers are REJECTED; compliance: next N are BLOCKED
	TimeoutNext int     `json:"timeout_next"` // bank: next N transfers are processed but the response is delayed past the client timeout
}

// Transfer is a bank transfer as recorded by the mock.
type Transfer struct {
	TransferID  string `json:"transfer_id"`
	Reference   string `json:"reference"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
}

// Server holds the mock state.
type Server struct {
	mu        sync.Mutex
	cfg       map[string]*Config
	responses map[string]map[string]any // service|key -> stored response
	transfers map[string]Transfer       // idempotency key -> transfer
	seeded    []Transfer                // settled entries inserted directly by an admin
	quotes    map[string]string         // quote id -> ACTIVE | CANCELLED
	calls     map[string]int            // service -> total calls
	replays   map[string]int            // service -> idempotent replays served
	inflight  int
	hang      time.Duration
	seq       int
}

var services = []string{"fx", "compliance", "bank"}

// New returns a mock server. hang is how long a TimeoutNext call delays its response.
func New(hang time.Duration) *Server {
	s := &Server{hang: hang}
	s.reset()
	return s
}

func (s *Server) reset() {
	s.cfg = map[string]*Config{}
	s.calls = map[string]int{}
	s.replays = map[string]int{}
	for _, n := range services {
		s.cfg[n] = &Config{}
	}
	s.responses = map[string]map[string]any{}
	s.transfers = map[string]Transfer{}
	s.seeded = nil
	s.quotes = map[string]string{}
}

// Handler returns the HTTP handler for all three mocks plus /admin.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /fx/quote", s.fxQuote)
	mux.HandleFunc("POST /fx/cancel", s.fxCancel)
	mux.HandleFunc("POST /compliance/screen", s.screen)
	mux.HandleFunc("POST /bank/transfer", s.transfer)
	mux.HandleFunc("GET /bank/settled", s.settled)
	mux.HandleFunc("GET /admin/state", s.state)
	mux.HandleFunc("POST /admin/{service}/config", s.setConfig)
	// reset clears injection config only: transfers, quotes and replay records are the bank's books.
	mux.HandleFunc("POST /admin/reset", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		for _, n := range services {
			s.cfg[n] = &Config{}
		}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "config reset"})
	})
	mux.HandleFunc("POST /admin/bank/clear-seeded", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.seeded = nil
		s.mu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "seeded entries cleared"})
	})
	mux.HandleFunc("POST /admin/bank/seed-settled", s.seedSettled)
	return logging(mux)
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("mock request", "method", r.Method, "path", r.URL.Path, "idempotency_key", r.Header.Get("Idempotency-Key"), "ms", time.Since(start).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// begin applies shared injection (calls counter, latency, failures). It returns false when it already replied.
func (s *Server) begin(w http.ResponseWriter, svc string) bool {
	s.mu.Lock()
	s.calls[svc]++
	c := *s.cfg[svc]
	fail := false
	if s.cfg[svc].ErrorNext > 0 {
		s.cfg[svc].ErrorNext--
		fail = true
	} else if c.FailRate > 0 && rand.Float64() < c.FailRate {
		fail = true
	}
	s.mu.Unlock()
	if c.LatencyMS > 0 {
		time.Sleep(time.Duration(c.LatencyMS) * time.Millisecond)
	}
	if fail {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": svc + " injected failure"})
		return false
	}
	return true
}

func (s *Server) replay(svc, key string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.responses[svc+"|"+key]; ok {
		s.replays[svc]++
		return r, true
	}
	return nil, false
}

func (s *Server) remember(svc, key string, resp map[string]any) {
	s.mu.Lock()
	s.responses[svc+"|"+key] = resp
	s.mu.Unlock()
}

func (s *Server) nextID(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return fmt.Sprintf("%s_%06d", prefix, s.seq)
}

func (s *Server) fxQuote(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if resp, ok := s.replay("fx", key); ok {
		writeJSON(w, 200, resp)
		return
	}
	if !s.begin(w, "fx") {
		return
	}
	var in struct {
		From, To    string
		AmountMinor int64 `json:"amount_minor"`
	}
	_ = decode(r, &in)
	id := s.nextID("fxq")
	resp := map[string]any{"quote_id": id, "rate": 0.65, "from": in.From, "to": in.To, "expires_at": time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)}
	s.mu.Lock()
	s.quotes[id] = "ACTIVE"
	s.mu.Unlock()
	s.remember("fx", key, resp)
	writeJSON(w, 200, resp)
}

func (s *Server) fxCancel(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if resp, ok := s.replay("fx", key); ok {
		writeJSON(w, 200, resp)
		return
	}
	if !s.begin(w, "fx") {
		return
	}
	var in struct {
		QuoteID string `json:"quote_id"`
	}
	_ = decode(r, &in)
	s.mu.Lock()
	if _, ok := s.quotes[in.QuoteID]; ok {
		s.quotes[in.QuoteID] = "CANCELLED"
	}
	s.mu.Unlock()
	resp := map[string]any{"quote_id": in.QuoteID, "status": "CANCELLED"}
	s.remember("fx", key, resp)
	writeJSON(w, 200, resp)
}

func (s *Server) screen(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if resp, ok := s.replay("compliance", key); ok {
		writeJSON(w, 200, resp)
		return
	}
	if !s.begin(w, "compliance") {
		return
	}
	decision := "CLEAR"
	s.mu.Lock()
	if s.cfg["compliance"].RejectNext > 0 {
		s.cfg["compliance"].RejectNext--
		decision = "BLOCKED"
	}
	s.mu.Unlock()
	resp := map[string]any{"decision": decision, "screened_at": time.Now().UTC().Format(time.RFC3339)}
	s.remember("compliance", key, resp)
	writeJSON(w, 200, resp)
}

func (s *Server) transfer(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	s.mu.Lock()
	s.calls["bank"]++
	if t, ok := s.transfers[key]; ok { // idempotent replay: never send money twice
		s.replays["bank"]++
		hang := false
		if c := s.cfg["bank"]; c.TimeoutNext > 0 { // the reply of a replay can be lost too
			c.TimeoutNext--
			hang = true
		}
		s.mu.Unlock()
		if hang {
			time.Sleep(s.hang)
		}
		writeJSON(w, 200, t)
		return
	}
	s.calls["bank"]-- // begin() counts this call
	s.mu.Unlock()
	if !s.begin(w, "bank") {
		return
	}
	var in struct {
		Reference   string `json:"reference"`
		AmountMinor int64  `json:"amount_minor"`
		Currency    string `json:"currency"`
	}
	_ = decode(r, &in)

	s.mu.Lock()
	c := s.cfg["bank"]
	t := Transfer{Reference: in.Reference, AmountMinor: in.AmountMinor, Currency: in.Currency, Status: "SETTLED"}
	if c.RejectNext > 0 {
		c.RejectNext--
		t.Status, t.Reason = "REJECTED", "insufficient correspondent funds (injected)"
	}
	hang := false
	if c.TimeoutNext > 0 {
		c.TimeoutNext--
		hang = true
	}
	s.seq++
	t.TransferID = fmt.Sprintf("txn_%06d", s.seq)
	s.transfers[key] = t // processed and recorded BEFORE the response, like a real rail whose reply can be lost
	s.inflight++
	s.mu.Unlock()

	if hang {
		time.Sleep(s.hang)
	}
	s.mu.Lock()
	s.inflight--
	s.mu.Unlock()
	writeJSON(w, 200, t)
}

func (s *Server) settled(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Transfer{}, s.seeded...)
	for _, t := range s.transfers {
		if t.Status == "SETTLED" {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TransferID < out[j].TransferID })
	writeJSON(w, 200, map[string]any{"transfers": out})
}

func (s *Server) seedSettled(w http.ResponseWriter, r *http.Request) {
	var in Transfer
	if err := decode(r, &in); err != nil || in.Reference == "" {
		writeJSON(w, 400, map[string]string{"error": "reference and amount_minor required"})
		return
	}
	s.mu.Lock()
	s.seq++
	in.TransferID, in.Status = fmt.Sprintf("txn_seed_%03d", s.seq), "SETTLED"
	s.seeded = append(s.seeded, in)
	s.mu.Unlock()
	writeJSON(w, 200, in)
}

func (s *Server) setConfig(w http.ResponseWriter, r *http.Request) {
	svc := r.PathValue("service")
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cfg[svc]
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "unknown service"})
		return
	}
	var in Config
	if err := decode(r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	*c = in
	writeJSON(w, 200, c)
}

func (s *Server) state(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settled, rejected := 0, 0
	for _, t := range s.transfers {
		if t.Status == "SETTLED" {
			settled++
		} else {
			rejected++
		}
	}
	active, cancelled := 0, 0
	for _, st := range s.quotes {
		if st == "ACTIVE" {
			active++
		} else {
			cancelled++
		}
	}
	writeJSON(w, 200, map[string]any{
		"config":              s.cfg,
		"calls":               s.calls,
		"idempotent_replays":  s.replays,
		"bank_inflight":       s.inflight,
		"bank_transfers":      len(s.transfers),
		"bank_settled":        settled,
		"bank_rejected":       rejected,
		"bank_seeded_settled": len(s.seeded),
		"fx_quotes_active":    active,
		"fx_quotes_cancelled": cancelled,
	})
}
