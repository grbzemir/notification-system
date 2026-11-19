package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/provider"
)

type fakeStore struct {
	n          *domain.Notification
	claimErr   error
	sentID     string
	sentCtxErr error
	failed     string
	released   bool
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

func newN() *domain.Notification {
	return &domain.Notification{ID: uuid.New(), Recipient: "+905551234567", Channel: domain.ChannelSMS, Content: "hi", Attempts: 1}
}

func TestProcessSuccess(t *testing.T) {
	st, snd, lim := &fakeStore{n: newN()}, &fakeSender{resp: &provider.Response{MessageID: "m-1"}}, &fakeLimiter{}
	if err := NewProcessor(st, snd, lim, time.Minute).Process(context.Background(), st.n.ID); err != nil {
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
	if err := NewProcessor(st, snd, lim, time.Minute).Process(context.Background(), uuid.New()); err != nil {
		t.Fatalf("not claimable must be a quiet success, got %v", err)
	}
	if len(snd.got) != 0 || len(lim.channels) != 0 {
		t.Fatal("must neither send nor spend rate budget")
	}
}

func TestProcessReturnsErrorOnClaimInfraFailure(t *testing.T) {
	st := &fakeStore{claimErr: errors.New("db down")}
	snd := &fakeSender{}
	if err := NewProcessor(st, snd, &fakeLimiter{}, time.Minute).Process(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error so the message is requeued")
	}
	if len(snd.got) != 0 {
		t.Fatal("must not send")
	}
}

func TestProcessProviderFailureMarksFailedAndAcks(t *testing.T) {
	for name, perr := range map[string]error{
		"permanent": &provider.Error{StatusCode: 400, Err: errors.New("bad")},
		"retryable": &provider.Error{StatusCode: 503, Retryable: true, Err: errors.New("down")},
	} {
		st, snd := &fakeStore{n: newN()}, &fakeSender{err: perr}
		if err := NewProcessor(st, snd, &fakeLimiter{}, time.Minute).Process(context.Background(), st.n.ID); err != nil {
			t.Fatalf("%s: err = %v", name, err)
		}
		if st.failed == "" || st.sentID != "" {
			t.Fatalf("%s: failed=%q sent=%q", name, st.failed, st.sentID)
		}
	}
}

func TestProcessRateLimitErrorReleasesClaimAndRequeues(t *testing.T) {
	st, snd := &fakeStore{n: newN()}, &fakeSender{}
	err := NewProcessor(st, snd, &fakeLimiter{err: context.Canceled}, time.Minute).Process(context.Background(), st.n.ID)
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
	if err := NewProcessor(st, snd, &fakeLimiter{}, time.Minute).Process(ctx, st.n.ID); err != nil {
		t.Fatal(err)
	}
	if st.sentID != "m-2" || st.sentCtxErr != nil {
		t.Fatalf("sentID=%q ctxErr=%v (result must be saved with a live context)", st.sentID, st.sentCtxErr)
	}
}

func TestMarkSentFailureRequeues(t *testing.T) {
	st := &fakeStore{n: newN(), markErr: errors.New("db down")}
	snd := &fakeSender{resp: &provider.Response{MessageID: "m"}}
	if err := NewProcessor(st, snd, &fakeLimiter{}, time.Minute).Process(context.Background(), st.n.ID); err == nil {
		t.Fatal("expected error")
	}
}
