// Package queue RabbitMQ topolojisini, mesaj formatını ve yayıncıyı (publisher) içerir.
package queue

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/grbzemir/notification-system/internal/domain"
)

const (
	// ExchangeName tüm bildirimlerin ilk gönderildiği direct exchange'dir. Routing key = kanal adı.
	ExchangeName = "notifications"

	// Başarısız/reddedilen mesajların gittiği dead letter exchange, kuyruk ve routing key.
	DeadLetterExchange = "notifications.dlx"
	DeadLetterQueue    = "notifications.dead"
	DeadLetterKey      = "dead"

	// MaxPriority kuyruğun desteklediği en yüksek öncelik değeridir.
	// RabbitMQ öncelik seviyesi başına ayrı bir iç kuyruk tutar; bu yüzden küçük tutulur (high/normal/low = 3/2/1).
	MaxPriority = 3
)

// QueueName bir kanalın ana kuyruğunun adıdır: "notifications.sms" gibi.
func QueueName(ch domain.Channel) string { return "notifications." + string(ch) }

// PriorityValue domain önceliğini AMQP öncelik değerine çevirir. Bilinmeyen değer "normal" sayılır.
func PriorityValue(p domain.Priority) uint8 {
	switch p {
	case domain.PriorityHigh:
		return 3
	case domain.PriorityLow:
		return 1
	default:
		return 2
	}
}

// Message kuyruğa konan mesajın gövdesidir. Bilerek küçüktür: içerik taşımaz.
// Worker içeriği ve güncel durumu (iptal edildi mi?) veritabanından okur, çünkü doğruluğun kaynağı odur.
type Message struct {
	ID       string          `json:"id"`
	Channel  domain.Channel  `json:"channel"`
	Priority domain.Priority `json:"priority"`
}

func NewMessage(n *domain.Notification) Message {
	return Message{ID: n.ID.String(), Channel: n.Channel, Priority: n.Priority}
}

func (m Message) Marshal() ([]byte, error) { return json.Marshal(m) }

// DecodeMessage kuyruktan gelen gövdeyi okur ve doğrular. Geçersiz mesaj "zehirli" (poison) sayılır.
func DecodeMessage(body []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(body, &m); err != nil {
		return Message{}, fmt.Errorf("decode message: %w", err)
	}
	if _, err := uuid.Parse(m.ID); err != nil {
		return Message{}, errors.New("decode message: id is not a valid UUID")
	}
	if !m.Channel.Valid() {
		return Message{}, errors.New("decode message: invalid channel")
	}
	if m.Priority == "" {
		m.Priority = domain.PriorityNormal
	}
	if !m.Priority.Valid() {
		return Message{}, errors.New("decode message: invalid priority")
	}
	return m, nil
}
