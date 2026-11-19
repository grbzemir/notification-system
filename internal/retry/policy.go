// Package retry başarısız gönderimlerin ne zaman ve kaç kez yeniden deneneceğini belirler.
package retry

import (
	"math/rand/v2"
	"time"
)

// Policy üstel geri çekilme (exponential backoff) + jitter politikasıdır.
type Policy struct {
	MaxAttempts int            // ilk deneme dahil toplam deneme sayısı
	Base        time.Duration  // ilk başarısızlıktan sonraki bekleme üst sınırı
	Max         time.Duration  // bekleme süresinin tavanı
	Rand        func() float64 // [0,1) rastgele sayı; nil ise math/rand kullanılır (testlerde sabitlenir)
}

func (p Policy) rnd() float64 {
	if p.Rand != nil {
		return p.Rand()
	}
	return rand.Float64()
}

// Exhausted, attempts kadar deneme yapılmışsa artık vazgeçilmesi gerektiğini söyler.
func (p Policy) Exhausted(attempts int) bool { return attempts >= p.MaxAttempts }

// Delay başarısız olan attempt'inci denemeden (1'den başlar) sonra ne kadar bekleneceğini hesaplar.
//
// Üst sınır: Base * 2^(attempt-1), Max ile sınırlı. Gerçek bekleme "equal jitter" ile [üst/2, üst) aralığından seçilir:
// tüm başarısız mesajlar aynı anda geri gelip sağlayıcıyı yeniden boğmasın (thundering herd), ama bekleme de sıfıra
// yaklaşmasın. minDelay (ör. sağlayıcının Retry-After değeri) varsa bekleme ondan kısa olamaz.
func (p Policy) Delay(attempt int, minDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	upper := p.Base
	for i := 1; i < attempt && upper < p.Max; i++ {
		upper *= 2
	}
	if upper > p.Max {
		upper = p.Max
	}
	half := upper / 2
	d := half + time.Duration(p.rnd()*float64(half))
	if d < minDelay {
		d = minDelay
	}
	return d
}
