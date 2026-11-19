package retry

import (
	"testing"
	"time"
)

func fixed(v float64) func() float64 { return func() float64 { return v } }

func TestDelayGrowsExponentiallyAndIsCapped(t *testing.T) {
	p := Policy{MaxAttempts: 8, Base: 10 * time.Second, Max: 60 * time.Second, Rand: fixed(0.9999999)}
	upper := []time.Duration{10, 20, 40, 60, 60, 60}
	for i, want := range upper {
		got := p.Delay(i+1, 0)
		want *= time.Second
		if got > want || got < want*99/100 { // rand≈1 -> üst sınıra çok yakın
			t.Errorf("attempt %d: got %v, want ≈%v", i+1, got, want)
		}
	}
}

func TestDelayJitterStaysInsideHalfToFullRange(t *testing.T) {
	for _, r := range []float64{0, 0.25, 0.5, 0.75, 0.9999} {
		p := Policy{Base: 8 * time.Second, Max: time.Minute, Rand: fixed(r)}
		d := p.Delay(2, 0) // üst sınır 16 sn
		if d < 8*time.Second || d >= 16*time.Second {
			t.Errorf("rand=%v: delay %v outside [8s,16s)", r, d)
		}
	}
}

func TestDelayJitterActuallyVaries(t *testing.T) {
	p := Policy{Base: 10 * time.Second, Max: time.Minute} // gerçek rastgelelik
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		seen[p.Delay(3, 0)] = true
	}
	if len(seen) < 10 {
		t.Fatalf("expected varied delays, got %d distinct values", len(seen))
	}
}

func TestDelayHonoursMinDelay(t *testing.T) {
	p := Policy{Base: time.Second, Max: time.Minute, Rand: fixed(0)}
	if d := p.Delay(1, 30*time.Second); d != 30*time.Second {
		t.Fatalf("delay = %v, want 30s (Retry-After)", d)
	}
}

func TestDelayHandlesOddInput(t *testing.T) {
	p := Policy{Base: time.Second, Max: time.Minute, Rand: fixed(0)}
	if p.Delay(0, 0) != p.Delay(1, 0) || p.Delay(-5, 0) != p.Delay(1, 0) {
		t.Fatal("attempt < 1 should behave like 1")
	}
	if d := p.Delay(10_000, 0); d > time.Minute { // taşma olmadan tavanda kalmalı
		t.Fatalf("huge attempt gave %v", d)
	}
}

func TestExhausted(t *testing.T) {
	p := Policy{MaxAttempts: 5}
	if p.Exhausted(4) || !p.Exhausted(5) || !p.Exhausted(6) {
		t.Fatal("Exhausted boundary wrong")
	}
}
