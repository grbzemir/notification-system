package circuitbreaker

import (
	"context"
	"errors"
	"sync"

	"github.com/grbzemir/notification-system/internal/provider"
)

// Inner sarmalanan gönderici (provider.Client).
type Inner interface {
	Send(ctx context.Context, r provider.Request) (*provider.Response, error)
}

// Sender her kanal için ayrı bir devre kesici kullanan dekoratördür: örneğin yalnızca e-posta altyapısı çökerse
// SMS ve push gönderimi etkilenmez.
type Sender struct {
	inner Inner
	cfg   Config

	mu       sync.Mutex
	breakers map[string]*Breaker
}

func NewSender(inner Inner, cfg Config) *Sender {
	return &Sender{inner: inner, cfg: cfg, breakers: map[string]*Breaker{}}
}

func (s *Sender) breaker(channel string) *Breaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.breakers[channel]
	if !ok {
		b = New("provider/"+channel, s.cfg)
		s.breakers[channel] = b
	}
	return b
}

func (s *Sender) Send(ctx context.Context, r provider.Request) (*provider.Response, error) {
	var resp *provider.Response
	err := s.breaker(r.Channel).Execute(func() error {
		var err error
		resp, err = s.inner.Send(ctx, r)
		return err
	}, Classify)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Classify sağlayıcı hatasını devre kesici açısından yorumlar:
//   - nil                       -> Success
//   - tekrar denenebilir hata   -> Failure (5xx, 429, timeout, bağlantı hatası)
//   - kalıcı 4xx                -> Success (sağlayıcı ayakta, yalnızca bu isteği reddetti)
//   - diğer (ör. ctx iptali)    -> Ignore
func Classify(err error) Outcome {
	if err == nil {
		return Success
	}
	var pe *provider.Error
	if !errors.As(err, &pe) {
		return Ignore
	}
	switch {
	case pe.Retryable:
		return Failure
	case pe.StatusCode != 0:
		return Success
	default:
		return Ignore
	}
}
