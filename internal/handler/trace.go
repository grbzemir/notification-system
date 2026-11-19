package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/grbzemir/notification-system/internal/telemetry"
)

// untraced yol listesi: uzun ömürlü WebSocket bağlantısı tek bir dev span olurdu; dokümantasyon sayfaları da gürültüdür.
var untraced = map[string]bool{
	"/api/v1/ws":    true,
	"/docs":         true,
	"/openapi.yaml": true,
}

// Trace her HTTP isteği için bir "server" span'ı açar.
//   - Gelen istekte traceparent başlığı varsa iz o istemcinin izinin devamı olur.
//   - Yanıta X-Trace-Id eklenir: Jaeger'da bu kimlikle arama yapılır.
//   - Span adı rota şablonudur ("GET /api/v1/notifications/{id}"), gerçek yol değil: yüksek kardinaliteyi önler.
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if untraced[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := telemetry.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("url.path", r.URL.Path),
			))
		defer span.End()

		if sc := span.SpanContext(); sc.IsValid() {
			w.Header().Set("X-Trace-Id", sc.TraceID().String())
		}

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		// Rota şablonu yönlendirme bittikten sonra bellidir.
		if rc := chi.RouteContext(ctx); rc != nil {
			if pattern := rc.RoutePattern(); pattern != "" {
				span.SetName(r.Method + " " + pattern)
				span.SetAttributes(attribute.String("http.route", pattern))
			}
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		if status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	})
}
