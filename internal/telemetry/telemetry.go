// Package telemetry OpenTelemetry ile dağıtık izlemeyi (distributed tracing) kurar.
//
// Bir bildirimin yolu: HTTP isteği (API) -> veritabanı -> kuyruk -> worker -> sağlayıcı. Bu süreçler ve kuyruk aracılığıyla
// bağlanan adımlar tek bir "trace" altında toplanır; Jaeger'da isteğin nerede ne kadar zaman harcadığı görülür.
//
// İzleme OTEL_EXPORTER_OTLP_ENDPOINT ayarlıysa açılır; değilse tüm çağrılar etkisizdir (no-op) ve neredeyse maliyetsizdir.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/grbzemir/notification-system"

// Setup izlemeyi başlatır ve kapanışta bekleyen span'ları gönderen shutdown fonksiyonunu döner.
// Yayılım (propagation) her zaman kurulur: izleme kapalı olsa bile gelen traceparent başlığı bozulmadan geçer.
func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		slog.Info("tracing disabled (OTEL_EXPORTER_OTLP_ENDPOINT is not set)")
		return func(context.Context) error { return nil }, nil
	}

	// Adres, OTEL_EXPORTER_OTLP_ENDPOINT ortam değişkeninden okunur (standart OpenTelemetry ayarı).
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp), // span'lar toplu ve arka planda gönderilir: istek yolunu yavaşlatmaz
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
	)
	otel.SetTracerProvider(tp)
	slog.Info("tracing enabled", "service", service)
	return tp.Shutdown, nil
}

// Start yeni bir span başlatır. Dönen context'i alt çağrılara geçirmek span'ları ebeveyn-çocuk olarak bağlar.
func Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name, opts...)
}

// RecordError hatayı span'a işler ve span'ı başarısız olarak işaretler.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// Traceparent ctx'teki aktif span'ın W3C traceparent değerini döner; izleme kapalıysa boş string.
// Veritabanına yazılır ve kuyruk/relay/retry sırasında izin sürdürülmesinde kullanılır.
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// ContextWithTraceparent kaydedilmiş traceparent'ı ctx'e uzak ebeveyn olarak yükler. Boş/bozuksa ctx değişmez.
func ContextWithTraceparent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

// TraceID ctx'teki trace kimliğini döner (yoksa boş).
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
