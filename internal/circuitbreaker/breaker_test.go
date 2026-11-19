package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grbzemir/notification-system/internal/provider"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTest(threshold int, timeout time.Duration) (*Breaker, *clock) {
	c := &clock{t: time.Unix(1_000_000, 0)}
	b := New("test", Config{FailureThreshold: threshold, OpenTimeout: timeout})
	b.now = c.now
	return b, c
}

var errBoom = errors.New("boom")

func classifyAll(o Outcome) func(error) Outcome { return func(error) Outcome { return o } }

func fail(b *Breaker) error {
	return b.Execute(func() error { return errBoom }, classifyAll(Failure))
}
func ok(b *Breaker) error { return b.Execute(func() error { return nil }, classifyAll(Success)) }

func TestOpensAfterConsecutiveFailures(t *testing.T) {
	b, _ := newTest(3, 10*time.Second)

	for i := 0; i < 3; i++ {
		if b.State() != Closed {
			t.Fatalf("call %d: state = %v, want closed", i, b.State())
		}
		_ = fail(b)
	}
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
}

func TestSuccessResetsFailureCount(t *testing.T) {
	b, _ := newTest(3, time.Second)

	_ = fail(b)
	_ = fail(b)
	_ = ok(b) // ardışık değil artık
	_ = fail(b)
	_ = fail(b)

	if b.State() != Closed {
		t.Fatalf("state = %v, want closed (failures were not consecutive)", b.State())
	}
}

func TestOpenRejectsWithoutCallingAndReportsRetryAfter(t *testing.T) {
	b, c := newTest(1, 10*time.Second)
	_ = fail(b)
	c.advance(4 * time.Second)

	called := false
	err := b.Execute(func() error { called = true; return nil }, classifyAll(Success))

	if called {
		t.Fatal("fn must not run while open")
	}
	var oe *OpenError
	if !errors.Is(err, ErrOpen) || !errors.As(err, &oe) {
		t.Fatalf("err = %v, want OpenError", err)
	}
	if oe.RetryAfter != 6*time.Second {
		t.Fatalf("RetryAfter = %v, want 6s", oe.RetryAfter)
	}
}

func TestHalfOpenSuccessCloses(t *testing.T) {
	b, c := newTest(1, 10*time.Second)
	_ = fail(b)
	c.advance(10 * time.Second)

	if err := ok(b); err != nil {
		t.Fatalf("probe should be allowed: %v", err)
	}
	if b.State() != Closed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

func TestHalfOpenFailureReopensWithFreshTimeout(t *testing.T) {
	b, c := newTest(1, 10*time.Second)
	_ = fail(b)
	c.advance(10 * time.Second)

	_ = fail(b) // deneme isteği de başarısız

	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
	err := ok(b)
	var oe *OpenError
	if !errors.As(err, &oe) || oe.RetryAfter != 10*time.Second {
		t.Fatalf("err = %v, want fresh 10s open window", err)
	}
}

func TestHalfOpenAllowsOnlyOneProbe(t *testing.T) {
	b, c := newTest(1, 10*time.Second)
	_ = fail(b)
	c.advance(10 * time.Second)

	var second error
	_ = b.Execute(func() error {
		// deneme sürerken ikinci istek gelir
		second = ok(b)
		return nil
	}, classifyAll(Success))

	if !errors.Is(second, ErrOpen) {
		t.Fatalf("second concurrent request err = %v, want ErrOpen", second)
	}
}

func TestIgnoredOutcomeDoesNotCountAndFreesProbe(t *testing.T) {
	b, c := newTest(2, 10*time.Second)
	_ = b.Execute(func() error { return errBoom }, classifyAll(Ignore))
	_ = b.Execute(func() error { return errBoom }, classifyAll(Ignore))
	if b.State() != Closed {
		t.Fatalf("ignored outcomes must not open the circuit, state = %v", b.State())
	}

	b2, c2 := newTest(1, time.Second)
	_ = fail(b2)
	c2.advance(time.Second)
	_ = b2.Execute(func() error { return errBoom }, classifyAll(Ignore)) // sonuçsuz deneme
	if err := ok(b2); err != nil {
		t.Fatalf("a new probe must be allowed after an ignored one: %v", err)
	}
	_ = c
}

// Devre açılmadan ÖNCE başlayan yavaş bir istek, açıldıktan sonra başarıyla bitse bile devreyi kapatmamalı.
func TestStaleResultFromOlderGenerationIsDropped(t *testing.T) {
	b, _ := newTest(1, 10*time.Second)

	_ = b.Execute(func() error {
		_ = fail(b) // bu sırada başka bir istek devreyi açar
		return nil
	}, classifyAll(Success))

	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Outcome
	}{
		{"nil", nil, Success},
		{"503", &provider.Error{StatusCode: 503, Retryable: true, Err: errBoom}, Failure},
		{"429", &provider.Error{StatusCode: 429, Retryable: true, Err: errBoom}, Failure},
		{"network", &provider.Error{Retryable: true, Err: errBoom}, Failure},
		{"400 means provider is alive", &provider.Error{StatusCode: 400, Err: errBoom}, Success},
		{"canceled", &provider.Error{Err: context.Canceled}, Ignore},
		{"foreign error", errBoom, Ignore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

type stubInner struct {
	calls int
	err   error
}

func (s *stubInner) Send(context.Context, provider.Request) (*provider.Response, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &provider.Response{MessageID: "m"}, nil
}

func TestSenderBreakersAreIndependentPerChannel(t *testing.T) {
	inner := &stubInner{err: &provider.Error{StatusCode: 503, Retryable: true, Err: errBoom}}
	s := NewSender(inner, Config{FailureThreshold: 2, OpenTimeout: time.Minute})
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		_, _ = s.Send(ctx, provider.Request{Channel: "email"})
	}
	callsBefore := inner.calls

	_, err := s.Send(ctx, provider.Request{Channel: "email"})
	if !errors.Is(err, ErrOpen) || inner.calls != callsBefore {
		t.Fatalf("email circuit should be open and not call provider: err=%v calls=%d", err, inner.calls)
	}

	inner.err = nil
	resp, err := s.Send(ctx, provider.Request{Channel: "sms"})
	if err != nil || resp.MessageID != "m" {
		t.Fatalf("sms must be unaffected: %v", err)
	}
}
