package config

import "os"

// Config uygulamanın tüm dış ayarlarını tutar. Değerler ortam değişkenlerinden okunur.
type Config struct {
	DatabaseURL string
	RabbitMQURL string
	RedisAddr   string
	ProviderURL string
	HTTPPort    string
}

// Load ortam değişkenlerini okur, tanımlı değilse yerel geliştirme varsayılanlarını kullanır.
func Load() Config {
	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://notif:notif@localhost:5432/notifications?sslmode=disable"),
		RabbitMQURL: getenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RedisAddr:   getenv("REDIS_ADDR", "localhost:6379"),
		ProviderURL: getenv("PROVIDER_URL", "http://localhost:9090/"),
		HTTPPort:    getenv("HTTP_PORT", "8080"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
