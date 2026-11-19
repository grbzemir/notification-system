package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/circuitbreaker"
	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/provider"
	"github.com/grbzemir/notification-system/internal/retry"
)

type fakeStore struct {
	n          *domain.Notification
	claimErr   error
	sentID     string
	sentCtxErr error
	failed     string
	released   bool
	retried    bool
	deferred   bool
	deferDelay time.Duration
	retryDelay time.Duration
	retryWhy   string
	retryErr   error
	markErr    error
	events     []string
}

func (s *fakeStore) Claim(context.Context, uuid.UUID, time.Duration) (*domain.Notification, error) {
	s.events = append(s.events, "claim")
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.n, nil
}
func (s *fakeStore) MarkSent(ctx context.Context, _ uuid.UUID, pid string) error {
	s.events = append(s.events, "sent")
	s.sentID, s.sentCtxErr = pid, ctx.Err()
	return s.markErr
}
func (s *fakeStore) MarkFailed(_ context.Context, _ uuid.UUID, reason string) error {
	s.events = append(s.events, "failed")
	s.failed = reason
	return nil
}
func (s *fakeStore) Retry(_ context.Context, _ uuid.UUID, reason string, d time.Duration) error {
	s.events = append(s.events, "retry")
	s.retried, s.retryWhy, s.retryDelay = true, reason, d
	return s.retryErr
}
func (s *fakeStore) Defer(_ context.Context, _ uuid.UUID, d time.Duration) error {
	s.events = append(s.events, "defer")
	s.deferred, s.deferDelay = true, d
	return nil
}
func (s *fakeStore) Release(context.Context, uuid.UUID) error {
	s.events = append(s.events, "release")
	s.released = true
	return nil
}

type fakeSender struct {
	got   []provider.Request
	resp  *provider.Response
	err   error
	after func() // gönderim sırasında yan etki (ör. ctx iptali)
}

func (f *fakeSender) Send(_ context.Context, r provider.Request) (*provider.Response, error) {
	f.got = append(f.got, r)
	if f.after != nil {
		f.after()
	}
	return f.resp, f.err
}

type fakeLimiter struct {
	err      error
	channels []string
}

func (l *fakeLimiter) Wait(_ context.Context, ch string) error {
	l.channels = append(l.channels, ch)
	return l.err
}

// Rand=0 -> bekleme üst sınırın tam yarısı: sonuç deterministik.
var testPolicy = retry.Policy{MaxAttempts: 3, Base: 10 * time.Second, Max: time.Minute, Rand: func() float64 { return 0 }}

func newN() *domain.Notification {
	return &domain.Notification{ID: uuid.New(), Recipient: "+905551234567", Channel: domain.ChannelSMS, Content: "hi", Attempts: 1}
}

func TestProcessSuccess(t *testing.T) {
	st, snd, lim := &fakeStore{n: newN()}, &fakeSender{resp: &provider.Response{MessageID: "m-1"}}, &fakeLimiter{}
	if err := NewProcessor(st, snd, lim, testPolicy, time.Minute).Process(context.Background(), st.n.ID); err != nil {
		t.Fatal(err)
	}
	if st.sentID != "m-1" {
		t.Fatalf("sentID = %q", st.sentID)
	}
	if len(snd.got) != 1 || snd.got[0] != (provider.Request{To: "+905551234567", Channel: "sms", Content: "hi"}) {
		t.Fatalf("sent = %+v", snd.got)
	}
	if len(lim.channels) != 1 || lim.channels[0] != "sms" {
		t.Fatalf("limiter channels = %v", lim.channels)
	}
	if want := []string{"claim", "sent"}; len(st.events) != 2 || st.events[0] != want[0] || st.events[1] != want[1] {
		t.Fatalf("events = %v", st.events)
	}
}

func TestProcessSkipsWhenNotClaimable(t *testing.T) {
	st, snd, lim := &fakeStore{claimErr: domain.ErrNotClaimable}, &fakeSender{}, &fakeLimiter{}
	if err := NewProcessor(st, snd, lim, testPolicy, time.Minute).Process(context.Background(), uuid.New()); err != nil {
		t.Fatalf("not claimable must be a quiet success, got %v", err)
	}
	if len(snd.got) != 0 || len(lim.channels) != 0 {
		t.Fatal("must neither send nor spend rate budget")
	}
}

func TestProcessReturnsErrorOnClaimInfraFailure(t *testing.T) {
	st := &fakeStore{claimErr: errors.New("db down")}
	snd := &fakeSender{}
	if err := NewProcessor(st, snd, &fakeLimiter{}, testPolicy, time.Minute).Process(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error so the message is requeued")
	}
	if len(snd.got) != 0 {
		t.Fatal("must not send")
	}
}

func process(st *fakeStore, snd *fakeSender) error {
	return NewProcessor(st, snd, &fakeLimiter{}, testPolicy, time.Minute).Process(context.Background(), st.n.ID)
}

func TestPermanentProviderErrorFailsImmediately(t *testing.T) {
	st := &fakeStore{n: newN()}
	if err := process(st, &fakeSender{err: &provider.Error{StatusCode: 400, Err: errors.New("bad")}}); err != nil {
		t.Fatal(err)
	}
	if st.failed == "" || st.retried {
		t.Fatalf("failed=%q retried=%v", st.failed, st.retried)
	}
}

func TestRetryableErrorSchedulesBackoffRetry(t *testing.T) {
	st := &fakeStore{n: newN()} // Attempts=1
	err := process(st, &fakeSender{err: &provider.Error{StatusCode: 503, Retryable: true, Err: errors.New("down")}})
	if err != nil {
		t.Fatalf("scheduling a retry is a successful outcome, got %v", err)
	}
	if !st.retried || st.failed != "" || st.sentID != "" {
		t.Fatalf("retried=%v failed=%q sent=%q", st.retried, st.failed, st.sentID)
	}
	if st.retryDelay != 5*time.Second { // 1. denemeden sonra: üst sınır 10s, Rand=0 -> 5s
		t.Fatalf("delay = %v, want 5s", st.retryDelay)
	}
}

func TestRetryDelayGrowsWithAttempts(t *testing.T) {
	n := newN()
	n.Attempts = 2
	st := &fakeStore{n: n}
	_ = process(st, &fakeSender{err: &provider.Error{StatusCode: 500, Retryable: true, Err: errors.New("x")}})
	if st.retryDelay != 10*time.Second { // üst sınır 20s -> 10s
		t.Fatalf("delay = %v, want 10s", st.retryDelay)
	}
}

func TestRetryGivesUpWhenAttemptsExhausted(t *testing.T) {
	n := newN()
	n.Attempts = 3 // MaxAttempts
	st := &fakeStore{n: n}
	if err := process(st, &fakeSender{err: &provider.Error{StatusCode: 503, Retryable: true, Err: errors.New("down")}}); err != nil {
		t.Fatal(err)
	}
	if st.retried || st.failed == "" {
		t.Fatalf("retried=%v failed=%q", st.retried, st.failed)
	}
	if !strings.Contains(st.failed, "gave up after 3 attempts") {
		t.Fatalf("reason = %q", st.failed)
	}
}

func TestRetryAfterFromProviderIsHonoured(t *testing.T) {
	st := &fakeStore{n: newN()}
	_ = process(st, &fakeSender{err: &provider.Error{StatusCode: 429, Retryable: true, RetryAfter: 45 * time.Second, Err: errors.New("slow")}})
	if st.retryDelay != 45*time.Second {
		t.Fatalf("delay = %v, want 45s", st.retryDelay)
	}
}

func TestFailureToScheduleRetryRequeuesMessage(t *testing.T) {
	st := &fakeStore{n: newN(), retryErr: errors.New("db down")}
	if err := process(st, &fakeSender{err: &provider.Error{StatusCode: 503, Retryable: true, Err: errors.New("x")}}); err == nil {
		t.Fatal("expected error so the message is requeued")
	}
}

func TestProcessRateLimitErrorReleasesClaimAndRequeues(t *testing.T) {
	st, snd := &fakeStore{n: newN()}, &fakeSender{}
	err := NewProcessor(st, snd, &fakeLimiter{err: context.Canceled}, testPolicy, time.Minute).Process(context.Background(), st.n.ID)
	if err == nil {
		t.Fatal("expected error")
	}
	if !st.released || len(snd.got) != 0 {
		t.Fatalf("released=%v sent=%d", st.released, len(snd.got))
	}
}

func TestResultIsRecordedEvenIfContextCancelledDuringSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	st := &fakeStore{n: newN()}
	snd := &fakeSender{resp: &provider.Response{MessageID: "m-2"}, after: cancel}
	if err := NewProcessor(st, snd, &fakeLimiter{}, testPolicy, time.Minute).Process(ctx, st.n.ID); err != nil {
		t.Fatal(err)
	}
	if st.sentID != "m-2" || st.sentCtxErr != nil {
		t.Fatalf("sentID=%q ctxErr=%v (result must be saved with a live context)", st.sentID, st.sentCtxErr)
	}
}

func TestMarkSentFailureRequeues(t *testing.T) {
	st := &fakeStore{n: newN(), markErr: errors.New("db down")}
	snd := &fakeSender{resp: &provider.Response{MessageID: "m"}}
	if err := NewProcessor(st, snd, &fakeLimiter{}, testPolicy, time.Minute).Process(context.Background(), st.n.ID); err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenCircuitDefersWithoutSpendingAnAttempt(t *testing.T) {
	st := &fakeStore{n: newN()}
	open := &circuitbreaker.OpenError{RetryAfter: 20 * time.Second}

	if err := process(st, &fakeSender{err: open}); err != nil {
		t.Fatalf("deferring is a successful outcome (ack), got %v", err)
	}
	if !st.deferred || st.deferDelay != 20*time.Second {
		t.Fatalf("deferred=%v delay=%v, want deferred for 20s", st.deferred, st.deferDelay)
	}
	if st.retried || st.failed != "" || st.sentID != "" || st.released {
		t.Fatalf("must only defer: retried=%v failed=%q sent=%q released=%v", st.retried, st.failed, st.sentID, st.released)
	}
}

func TestOpenCircuitDeferHasMinimumDelay(t *testing.T) {
	st := &fakeStore{n: newN()}
	_ = process(st, &fakeSender{err: &circuitbreaker.OpenError{RetryAfter: 100 * time.Millisecond}})
	if st.deferDelay != minCircuitDefer {
		t.Fatalf("delay = %v, want %v", st.deferDelay, minCircuitDefer)
	}
}

func TestOpenCircuitDeferEvenWhenAttemptsWouldBeExhausted(t *testing.T) {
	n := newN()
	n.Attempts = 3 // MaxAttempts: normal bir hata burada failed olurdu
	st := &fakeStore{n: n}
	_ = process(st, &fakeSender{err: &circuitbreaker.OpenError{RetryAfter: 5 * time.Second}})
	if !st.deferred || st.failed != "" {
		t.Fatalf("outage must not fail notifications: deferred=%v failed=%q", st.deferred, st.failed)
	}
}
