package queue

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
)

// tableCarrier AMQP mesaj başlıklarını OpenTelemetry'nin yayılım arayüzüne (TextMapCarrier) uyarlar.
// İz bağlamı (traceparent) mesajla birlikte kuyruktan geçer; worker, API'de başlayan izi kaldığı yerden sürdürür.
type tableCarrier amqp.Table

func (c tableCarrier) Get(key string) string {
	if v, ok := c[key].(string); ok {
		return v
	}
	return ""
}

func (c tableCarrier) Set(key, value string) { c[key] = value }

func (c tableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// injectHeaders ctx'teki aktif span'ın bağlamını yeni bir başlık tablosuna yazar.
func injectHeaders(ctx context.Context) amqp.Table {
	h := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, tableCarrier(h))
	return h
}

// extractContext mesaj başlıklarındaki iz bağlamını ctx'e uzak ebeveyn olarak yükler.
func extractContext(ctx context.Context, h amqp.Table) context.Context {
	if h == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, tableCarrier(h))
}
