// Package realtime WebSocket istemcilerine olay dağıtımının taşıyıcıdan bağımsız çekirdeğidir.
// Ağ/WebSocket detayları handler katmanındadır; burada yalnızca "kim hangi bildirimi dinliyor" ve
// "yavaş istemci diğerlerini yavaşlatmasın" mantığı vardır.
package realtime

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/grbzemir/notification-system/internal/events"
)

// MaxSubscriptions bir istemcinin aynı anda dinleyebileceği en fazla bildirim sayısıdır.
const MaxSubscriptions = 100

var ErrTooManySubscriptions = errors.New("too many subscriptions (max 100)")

// Client tek bir WebSocket bağlantısının hub tarafındaki temsilidir.
type Client struct {
	send chan []byte
	done chan struct{}
	once sync.Once

	mu  sync.RWMutex
	ids map[string]struct{}
	all bool
}

// NewClient buffer: istemci yavaşlarsa kuyrukta bekletilecek en fazla mesaj sayısı.
func NewClient(buffer int) *Client {
	return &Client{send: make(chan []byte, buffer), done: make(chan struct{}), ids: map[string]struct{}{}}
}

// Send yazma döngüsünün tükettiği giden mesaj kuyruğudur.
func (c *Client) Send() <-chan []byte { return c.send }

// Done istemci hub tarafından atıldığında (yavaş tüketici) kapanır.
func (c *Client) Done() <-chan struct{} { return c.done }

// kick istemciyi atar (idempotent).
func (c *Client) kick() { c.once.Do(func() { close(c.done) }) }

// Offer mesajı bloklamadan kuyruğa koyar. Kuyruk doluysa istemci yavaştır: atılır ve false döner.
func (c *Client) Offer(msg []byte) bool {
	select {
	case c.send <- msg:
		return true
	default:
		c.kick()
		return false
	}
}

// Subscribe verilen bildirim kimliklerini dinlemeye başlar. Sınır aşılırsa hiçbiri eklenmez.
func (c *Client) Subscribe(ids []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := len(c.ids)
	for _, id := range ids {
		if _, ok := c.ids[id]; !ok {
			next++
		}
	}
	if next > MaxSubscriptions {
		return ErrTooManySubscriptions
	}
	for _, id := range ids {
		c.ids[id] = struct{}{}
	}
	return nil
}

func (c *Client) Unsubscribe(ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		delete(c.ids, id)
	}
}

// SetAll true ise istemci tüm bildirimlerin olaylarını alır.
func (c *Client) SetAll(v bool) {
	c.mu.Lock()
	c.all = v
	c.mu.Unlock()
}

func (c *Client) wants(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.all {
		return true
	}
	_, ok := c.ids[id]
	return ok
}

// Hub bağlı istemcileri tutar ve olayları ilgilenenlere dağıtır.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[*Client]struct{}{}} }

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Count bağlı istemci sayısıdır.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast olayı yalnızca onu dinleyen istemcilere iletir. JSON bir kez üretilir. Hiçbir istemci diğerini bekletmez.
func (h *Hub) Broadcast(e events.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.wants(e.ID) {
			c.Offer(data)
		}
	}
}
