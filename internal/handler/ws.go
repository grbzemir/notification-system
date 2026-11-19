package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
	"github.com/grbzemir/notification-system/internal/events"
	"github.com/grbzemir/notification-system/internal/realtime"
)

const (
	wsSendBuffer   = 32
	wsReadLimit    = 4096
	wsWriteTimeout = 5 * time.Second
	wsPingInterval = 30 * time.Second
)

// NotificationGetter abone olunan bildirimin o anki durumunu okur.
type NotificationGetter interface {
	Get(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
}

// WSHandler canlı durum güncellemelerini WebSocket ile iletir.
//
//	GET /api/v1/ws?id=<uuid>&id=<uuid>   bağlanırken dinlenecek bildirimler
//	GET /api/v1/ws?all=true              tüm bildirimler
//
// Bağlantı açıkken istemci komut gönderebilir:
//
//	{"action":"subscribe","ids":["<uuid>", ...]}
//	{"action":"unsubscribe","ids":["<uuid>", ...]}
type WSHandler struct {
	hub *realtime.Hub
	svc NotificationGetter
}

func NewWSHandler(hub *realtime.Hub, svc NotificationGetter) *WSHandler {
	return &WSHandler{hub: hub, svc: svc}
}

type wsCommand struct {
	Action string   `json:"action"`
	IDs    []string `json:"ids"`
}

func (h *WSHandler) Serve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	initial, ok := validIDs(q["id"])
	if !ok || len(initial) > realtime.MaxSubscriptions {
		writeError(w, http.StatusBadRequest, "invalid_id", "id must be a valid UUID (at most 100)", nil)
		return
	}

	// Origin denetimi kütüphanenin varsayılanıdır: tarayıcıdan gelen farklı kökenli (cross-origin) bağlantı reddedilir.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept yanıtı kendisi yazar
	}
	defer conn.CloseNow()
	conn.SetReadLimit(wsReadLimit)

	client := realtime.NewClient(wsSendBuffer)
	client.SetAll(q.Get("all") == "true")
	h.hub.Register(client)
	defer h.hub.Unregister(client)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go h.writeLoop(ctx, cancel, conn, client)

	// Olay kaçırmamak için önce abone ol, sonra anlık durumu gönder: abonelik ile okuma arasında
	// gelen olay hub'dan ulaşır, sonrasındaki anlık durum onu en fazla tekrar eder (kaybetmez).
	if err := client.Subscribe(initial); err == nil {
		h.sendSnapshots(ctx, client, initial)
	}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return // bağlantı kapandı ya da ctx iptal oldu
		}
		h.handleCommand(ctx, client, data)
	}
}

func (h *WSHandler) handleCommand(ctx context.Context, c *realtime.Client, data []byte) {
	var cmd wsCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		reply(c, map[string]string{"error": "invalid JSON command"})
		return
	}
	ids, ok := validIDs(cmd.IDs)
	if !ok {
		reply(c, map[string]string{"error": "ids must be valid UUIDs"})
		return
	}
	switch cmd.Action {
	case "subscribe":
		if err := c.Subscribe(ids); err != nil {
			reply(c, map[string]string{"error": err.Error()})
			return
		}
		h.sendSnapshots(ctx, c, ids)
	case "unsubscribe":
		c.Unsubscribe(ids)
	default:
		reply(c, map[string]string{"error": `action must be "subscribe" or "unsubscribe"`})
	}
}

// sendSnapshots abone olunan bildirimlerin o anki durumunu gönderir; böylece istemci, bildirimi oluşturduktan
// sonra abone olana kadar kaçan olayları (ör. çoktan "sent" olmuş) yine de öğrenir.
func (h *WSHandler) sendSnapshots(ctx context.Context, c *realtime.Client, ids []string) {
	for _, s := range ids {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		n, err := h.svc.Get(ctx, id)
		if err != nil {
			continue // bulunamadı ya da geçici hata: olaylar yine gelir
		}
		reply(c, events.FromNotification(n))
	}
}

func reply(c *realtime.Client, v any) {
	if data, err := json.Marshal(v); err == nil {
		c.Offer(data)
	}
}

// writeLoop giden mesajları yazar ve bağlantıyı canlı tutmak için periyodik ping atar.
// Tek bir goroutine yazdığı için yazmalar sıralıdır.
func (h *WSHandler) writeLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, c *realtime.Client) {
	defer cancel()
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.Done():
			slog.Warn("websocket client too slow, closing")
			_ = conn.Close(websocket.StatusPolicyViolation, "slow consumer")
			return
		case msg := <-c.Send():
			wctx, wcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, wsWriteTimeout)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

func validIDs(raw []string) ([]string, bool) {
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, false
		}
		out = append(out, id.String()) // kanonik küçük harfli biçim: olaydaki id ile eşleşsin
	}
	return out, true
}
