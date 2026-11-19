// Package provider bildirimleri dış sağlayıcıya (webhook.site veya mock provider) HTTP ile gönderir.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Request sağlayıcının beklediği gövdedir (case dokümanındaki örnek: to, channel, content).
type Request struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
}

// Response sağlayıcının başarılı cevabıdır: 202 Accepted + messageId.
type Response struct {
	MessageID string    `json:"messageId"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// Error sağlayıcı çağrısının başarısızlığını ve TEKRAR DENENEBİLİR olup olmadığını taşır.
// Worker (Adım 8-9) yalnızca bu bilgiye bakarak "retry mı, kalıcı hata mı?" kararını verir.
type Error struct {
	StatusCode int           // 0 ise HTTP cevabı alınamadı (ağ hatası, timeout)
	Retryable  bool          // true: geçici sorun, tekrar denemeye değer
	RetryAfter time.Duration // sağlayıcı 429'da "şu kadar sonra dene" dediyse
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("provider unreachable (retryable=%t): %v", e.Retryable, e.Err)
	}
	return fmt.Sprintf("provider returned %d (retryable=%t): %v", e.StatusCode, e.Retryable, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// IsRetryable herhangi bir hatanın tekrar denenebilir olup olmadığını söyler.
// Provider hatası değilse (ör. context iptali) güvenli tarafta kalıp denenemez sayar.
func IsRetryable(err error) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.Retryable
}

// Client sağlayıcıya istek atar. Zaman aşımı zorunludur: sağlayıcı yanıt vermezse worker sonsuza dek asılı kalmasın.
type Client struct {
	url  string
	http *http.Client
}

func NewClient(url string, timeout time.Duration) *Client {
	return &Client{url: url, http: &http.Client{Timeout: timeout}}
}

// Send bildirimi sağlayıcıya iletir. Başarısızlıkta *Error döner.
func (c *Client) Send(ctx context.Context, r Request) (*Response, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, &Error{Err: err} // kodlama hatası tekrarla düzelmez
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// Bağlantı reddi, DNS, timeout: karşı taraf geçici olarak ulaşılamaz olabilir -> tekrar dene.
		// Ama bizim ctx'imiz iptal edildiyse (kapanış) tekrar denemenin anlamı yok.
		return nil, &Error{Retryable: ctx.Err() == nil, Err: err}
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // en fazla 1 MB oku

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out Response
		// Gövde yoksa/bozuksa bile 2xx kabul edilmiştir; bildirimi "gönderilmedi" saymak çift gönderime yol açar.
		_ = json.Unmarshal(payload, &out)
		return &out, nil
	}

	pe := &Error{StatusCode: resp.StatusCode, Err: fmt.Errorf("%s", truncate(string(payload), 200))}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		pe.Retryable = true
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			pe.RetryAfter = time.Duration(s) * time.Second
		}
	case resp.StatusCode >= 500:
		pe.Retryable = true
	default:
		pe.Retryable = false // 4xx: istek hatalı, aynı istek tekrar da hatalı olur
	}
	return nil, pe
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
