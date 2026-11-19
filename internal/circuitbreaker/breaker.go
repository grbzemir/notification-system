// Package circuitbreaker sağlayıcı çöktüğünde ona istek yağdırmayı keser (devre kesici).
//
// Üç durum:
//
//	Closed   -> normal; istekler geçer, ardışık hatalar sayılır
//	Open     -> istekler HİÇ gönderilmez, anında *OpenError döner (sağlayıcıya nefes aldırır)
//	HalfOpen -> bekleme bitti; tek bir deneme isteği geçer. Başarılıysa Closed, değilse yeniden Open
package circuitbreaker

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	default:
		return "half-open"
	}
}

// Outcome bir çağrının sağlayıcı sağlığı açısından anlamını söyler.
type Outcome int

const (
	Success Outcome = iota // sağlayıcı sağlıklı cevap verdi
	Failure                // sağlayıcı sorunlu (5xx, timeout, bağlantı hatası)
	Ignore                 // sağlayıcı hakkında bilgi yok (ör. bizim ctx'imiz iptal oldu)
)

// ErrOpen devre açıkken döner (errors.Is ile kontrol edilir).
var ErrOpen = errors.New("circuit breaker is open")

// OpenError devre açıkken ne kadar sonra tekrar denenebileceğini taşır.
type OpenError struct {
	RetryAfter time.Duration
}

func (e *OpenError) Error() string {
	return "circuit breaker is open; retry after " + e.RetryAfter.Round(time.Millisecond).String()
}

func (e *OpenError) Is(target error) bool { return target == ErrOpen }

// probeBusyRetry yarı açık durumda deneme isteği sürerken gelen diğer istekler için önerilen bekleme.
const probeBusyRetry = time.Second

type Config struct {
	FailureThreshold int           // Closed'da bu kadar ARDIŞIK hata devreyi açar
	OpenTimeout      time.Duration // Open'da bu süre sonra yarı açığa geçilir
}

type Breaker struct {
	name string
	cfg  Config
	now  func() time.Time // testlerde sahte saat

	mu         sync.Mutex
	state      State
	failures   int
	openedAt   time.Time
	probing    bool   // yarı açıkta deneme isteği sürüyor mu
	generation uint64 // her durum değişiminde artar; eski durumdan kalan sonuçları yok saymak için
}

func New(name string, cfg Config) *Breaker {
	if cfg.FailureThreshold < 1 {
		cfg.FailureThreshold = 5
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = 30 * time.Second
	}
	return &Breaker{name: name, cfg: cfg, now: time.Now}
}

// State anlık durumu döner (Open'dan HalfOpen'a geçiş bir sonraki istekte olur).
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Execute fn'i devre izin veriyorsa çalıştırır ve sonucunu classify ile değerlendirir.
// Devre açıksa fn çağrılmaz, *OpenError döner.
func (b *Breaker) Execute(fn func() error, classify func(error) Outcome) error {
	gen, err := b.before()
	if err != nil {
		return err
	}
	err = fn()
	b.after(gen, classify(err))
	return err
}

func (b *Breaker) before() (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Open:
		elapsed := b.now().Sub(b.openedAt)
		if elapsed < b.cfg.OpenTimeout {
			return 0, &OpenError{RetryAfter: b.cfg.OpenTimeout - elapsed}
		}
		b.transition(HalfOpen)
		b.probing = true
	case HalfOpen:
		if b.probing {
			return 0, &OpenError{RetryAfter: probeBusyRetry}
		}
		b.probing = true
	}
	return b.generation, nil
}

func (b *Breaker) after(gen uint64, o Outcome) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Devre, bu istek başladıktan sonra durum değiştirdiyse (ör. yavaş bir istek devre açıldıktan sonra bitti) sonucu yok say.
	if gen != b.generation {
		return
	}

	switch b.state {
	case Closed:
		switch o {
		case Success:
			b.failures = 0
		case Failure:
			b.failures++
			if b.failures >= b.cfg.FailureThreshold {
				b.trip()
			}
		}
	case HalfOpen:
		switch o {
		case Success:
			b.failures = 0
			b.transition(Closed)
		case Failure:
			b.trip()
		case Ignore:
			b.probing = false // deneme sonuçsuz kaldı; sıradaki istek yeniden denesin
		}
	}
}

func (b *Breaker) trip() {
	b.openedAt = b.now()
	b.transition(Open)
}

// transition durumu değiştirir; çağıran kilidi tutmalıdır.
func (b *Breaker) transition(to State) {
	slog.Warn("circuit breaker state change", "breaker", b.name, "from", b.state.String(), "to", to.String())
	b.state = to
	b.probing = false
	b.generation++
}
