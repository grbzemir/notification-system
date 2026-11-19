// Command mockprovider gerçek bir SMS/email/push sağlayıcısını taklit eder.
// Retry, circuit breaker ve rate limit davranışlarını yerelde, deterministik biçimde denemek için vardır.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type request struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
}

type server struct {
	failRate float64       // 0..1: rastgele 503 oranı
	latency  time.Duration // her isteğe eklenen sabit gecikme
	received atomic.Int64
	accepted atomic.Int64
}

// İçeriğe gömülen özel işaretler, testte hata senaryosunu TEK bir bildirim için seçmeyi sağlar.
//
//	[fail-400]  -> 400 (kalıcı hata, tekrar denenmemeli)
//	[fail-500]  -> 500 (geçici hata, tekrar denenmeli)
//	[fail-429]  -> 429 + Retry-After: 2
//	[slow]      -> 3 sn gecikme (client timeout testi)
func (s *server) handleSend(w http.ResponseWriter, r *http.Request) {
	s.received.Add(1)

	var req request
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if req.To == "" || req.Channel == "" || req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "to, channel and content are required"})
		return
	}

	if s.latency > 0 {
		time.Sleep(s.latency)
	}
	if strings.Contains(req.Content, "[slow]") {
		time.Sleep(3 * time.Second)
	}

	switch {
	case strings.Contains(req.Content, "[fail-400]"):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rejected by provider"})
		return
	case strings.Contains(req.Content, "[fail-429]"):
		w.Header().Set("Retry-After", "2")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "slow down"})
		return
	case strings.Contains(req.Content, "[fail-500]"):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "provider exploded"})
		return
	case s.failRate > 0 && mrand.Float64() < s.failRate:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "temporarily unavailable"})
		return
	}

	s.accepted.Add(1)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"messageId": newID(),
		"status":    "accepted",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int64{"received": s.received.Load(), "accepted": s.accepted.Load()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	s := &server{}
	if v, err := strconv.ParseFloat(os.Getenv("MOCK_FAIL_RATE"), 64); err == nil && v >= 0 && v <= 1 {
		s.failRate = v
	}
	if v, err := strconv.Atoi(os.Getenv("MOCK_LATENCY_MS")); err == nil && v > 0 {
		s.latency = time.Duration(v) * time.Millisecond
	}
	port := os.Getenv("MOCK_PORT")
	if port == "" {
		port = "9090"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", s.handleSend)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		slog.Info("mock provider listening", "addr", srv.Addr, "fail_rate", s.failRate, "latency", s.latency.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
