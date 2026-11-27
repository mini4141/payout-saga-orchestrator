// Package obs wires structured logging, OpenTelemetry tracing and Prometheus metrics.
package obs

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Env returns the environment variable key or def when unset.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// SetupLogger installs a JSON slog logger on stdout and returns it.
func SetupLogger(service string) *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	l := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", service)
	slog.SetDefault(l)
	return l
}

// SetupTracing installs an OpenTelemetry tracer provider. Spans are written as JSON
// to stderr when OTEL_TRACES_STDOUT=true; otherwise the provider is a no-op sink.
func SetupTracing(service string) func(context.Context) error {
	res := resource.NewSchemaless(semconv.ServiceName(service))
	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if os.Getenv("OTEL_TRACES_STDOUT") == "true" {
		exp, err := stdouttrace.New(stdouttrace.WithWriter(os.Stderr))
		if err == nil {
			opts = append(opts, sdktrace.WithBatcher(exp))
		}
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return tp.Shutdown
}

// Registry is the process-wide Prometheus registry.
var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
}

// Counter registers and returns a labelled counter.
func Counter(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	Registry.MustRegister(c)
	return c
}

// Histogram registers and returns a labelled histogram.
func Histogram(name, help string, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help}, labels)
	Registry.MustRegister(h)
	return h
}

// Mux returns a mux with /healthz and /metrics registered.
func Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}))
	return mux
}

// Serve runs a small HTTP server with /healthz and /metrics (for the worker and relay).
func Serve(addr string) {
	go func() {
		if err := http.ListenAndServe(addr, Mux()); err != nil {
			slog.Error("metrics server stopped", "err", err)
		}
	}()
}
