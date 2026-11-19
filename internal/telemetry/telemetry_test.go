package telemetry

import (
	"context"
	"regexp"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

var traceparentFormat = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$`)

func setupTracing(t *testing.T) {
	t.Helper()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestTraceparentRoundTripKeepsTraceID(t *testing.T) {
	setupTracing(t)
	ctx, span := Start(context.Background(), "root")
	defer span.End()

	tp := Traceparent(ctx)
	if !traceparentFormat.MatchString(tp) {
		t.Fatalf("traceparent = %q, want W3C format", tp)
	}

	restored := ContextWithTraceparent(context.Background(), tp)
	if TraceID(restored) == "" || TraceID(restored) != TraceID(ctx) {
		t.Fatalf("trace id after restore = %q, want %q", TraceID(restored), TraceID(ctx))
	}
}

func TestChildSpanSharesTraceID(t *testing.T) {
	setupTracing(t)
	ctx, parent := Start(context.Background(), "parent")
	defer parent.End()

	_, child := Start(ctx, "child")
	defer child.End()

	if child.SpanContext().TraceID() != parent.SpanContext().TraceID() {
		t.Fatal("child span must belong to the parent's trace")
	}
	if child.SpanContext().SpanID() == parent.SpanContext().SpanID() {
		t.Fatal("child span must have its own span id")
	}
}

func TestNoSpanMeansEmptyTraceparent(t *testing.T) {
	setupTracing(t)
	if tp := Traceparent(context.Background()); tp != "" {
		t.Fatalf("traceparent = %q, want empty", tp)
	}
	if TraceID(context.Background()) != "" {
		t.Fatal("no trace id expected without a span")
	}
}

func TestEmptyOrBadTraceparentLeavesContextUsable(t *testing.T) {
	setupTracing(t)
	ctx := context.Background()
	if got := ContextWithTraceparent(ctx, ""); got != ctx {
		t.Fatal("empty traceparent must return the same context")
	}
	if id := TraceID(ContextWithTraceparent(ctx, "garbage")); id != "" {
		t.Fatalf("bad traceparent must not produce a trace id, got %q", id)
	}
}
