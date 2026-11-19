package config

import (
	"os"
	"strconv"
	"time"
)

// Worker worker servisinin ayarlarıdır. Ortak bağlantı ayarları API ile aynı kaynaktan (Load) gelir.
type Worker struct {
	DatabaseURL string
	RabbitMQURL string
	RedisAddr   string
	ProviderURL string

	Concurrency     int           // her kanal için aynı anda işlenen mesaj sayısı (= prefetch)
	RateLimitPerSec int           // kanal başına saniyede en fazla gönderim
	ProviderTimeout time.Duration // sağlayıcı çağrısı zaman aşımı
	Lease           time.Duration // "processing" kaydının devralınabilmesi için gereken süre

	RetryMaxAttempts int           // ilk deneme dahil toplam deneme sayısı
	RetryBase        time.Duration // ilk başarısızlıktan sonraki bekleme üst sınırı
	RetryMax         time.Duration // bekleme tavanı

	CircuitFailureThreshold int           // bir kanalda bu kadar ardışık sağlayıcı hatası devreyi açar
	CircuitOpenTimeout      time.Duration // devre açık kaldıktan sonra tek deneme isteğine izin verilene kadar geçen süre
}

func LoadWorker() Worker {
	base := Load()
	return Worker{
		DatabaseURL:      base.DatabaseURL,
		RabbitMQURL:      base.RabbitMQURL,
		RedisAddr:        base.RedisAddr,
		ProviderURL:      base.ProviderURL,
		Concurrency:      positiveInt("WORKER_CONCURRENCY", 5),
		RateLimitPerSec:  positiveInt("RATE_LIMIT_PER_SEC", 100),
		ProviderTimeout:  time.Duration(positiveInt("PROVIDER_TIMEOUT_MS", 5000)) * time.Millisecond,
		Lease:            time.Duration(positiveInt("LEASE_SECONDS", 120)) * time.Second,
		RetryMaxAttempts: positiveInt("RETRY_MAX_ATTEMPTS", 5),
		RetryBase:        time.Duration(positiveInt("RETRY_BASE_MS", 5000)) * time.Millisecond,
		RetryMax:         time.Duration(positiveInt("RETRY_MAX_MS", 300000)) * time.Millisecond,

		CircuitFailureThreshold: positiveInt("CB_FAILURE_THRESHOLD", 5),
		CircuitOpenTimeout:      time.Duration(positiveInt("CB_OPEN_TIMEOUT_MS", 30000)) * time.Millisecond,
	}
}

// positiveInt ortam değişkenini okur; yoksa ya da geçersiz/sıfır/negatifse varsayılanı döner.
func positiveInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v > 0 {
		return v
	}
	return def
}
