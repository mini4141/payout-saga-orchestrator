// Package temporalx dials Temporal with retries and OpenTelemetry tracing.
package temporalx

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
)

// Dial connects to the Temporal frontend, retrying while the dev server starts.
func Dial(ctx context.Context, addr string) (client.Client, error) {
	tracer, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{})
	if err != nil {
		return nil, err
	}
	opts := client.Options{
		HostPort:     addr,
		Namespace:    "default",
		Logger:       nil,
		Interceptors: []interceptor.ClientInterceptor{tracer},
	}
	var lastErr error
	for i := 0; i < 60; i++ {
		c, err := client.DialContext(ctx, opts)
		if err == nil {
			return c, nil
		}
		lastErr = err
		slog.Warn("temporal not ready, retrying", "addr", addr, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("dial temporal %s: %w", addr, lastErr)
}
