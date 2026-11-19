package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func req() Request { return Request{To: "+905551234567", Channel: "sms", Content: "hi"} }

func TestSendSuccess(t *testing.T) {
	var got Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"messageId":"abc-123","status":"accepted","timestamp":"2026-01-02T03:04:05Z"}`))
	}))
	defer srv.Close()

	resp, err := NewClient(srv.URL, time.Second).Send(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if resp.MessageID != "abc-123" || resp.Status != "accepted" {
		t.Fatalf("resp = %+v", resp)
	}
	if got != req() {
		t.Fatalf("server got %+v", got)
	}
}

func TestSendSuccessWithEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if _, err := NewClient(srv.URL, time.Second).Send(context.Background(), req()); err != nil {
		t.Fatalf("2xx must be success even without body: %v", err)
	}
}

func TestSendErrorClassification(t *testing.T) {
	cases := []struct {
		status    int
		retryable bool
	}{
		{400, false}, {401, false}, {404, false}, {422, false},
		{429, true}, {500, true}, {502, true}, {503, true}, {504, true},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
		}))
		_, err := NewClient(srv.URL, time.Second).Send(context.Background(), req())
		srv.Close()
		if err == nil || IsRetryable(err) != c.retryable {
			t.Errorf("status %d: err=%v retryable=%v want %v", c.status, err, IsRetryable(err), c.retryable)
		}
	}
}

func TestRetryAfterIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, time.Second).Send(context.Background(), req())
	pe, ok := err.(*Error)
	if !ok || pe.RetryAfter != 7*time.Second {
		t.Fatalf("err = %#v", err)
	}
}

func TestTimeoutIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, 50*time.Millisecond).Send(context.Background(), req())
	if err == nil || !IsRetryable(err) {
		t.Fatalf("timeout should be retryable, err=%v", err)
	}
}

func TestConnectionRefusedIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // artık kimse dinlemiyor
	_, err := NewClient(url, time.Second).Send(context.Background(), req())
	if err == nil || !IsRetryable(err) {
		t.Fatalf("connection refused should be retryable, err=%v", err)
	}
}

func TestCancelledContextIsNotRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(srv.URL, time.Second).Send(ctx, req())
	if err == nil || IsRetryable(err) {
		t.Fatalf("cancelled ctx must not be retryable, err=%v", err)
	}
}

func TestIsRetryableOnForeignError(t *testing.T) {
	if IsRetryable(context.Canceled) || IsRetryable(nil) {
		t.Fatal("non-provider errors are not retryable")
	}
}
