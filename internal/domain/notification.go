package domain

import (
	"time"

	"github.com/google/uuid"
)

// Channel bildirimin gönderileceği kanaldır.
type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelPush  Channel = "push"
)

// Channels sistemdeki tüm kanalları listeler (worker'ları başlatırken kullanacağız).
var Channels = []Channel{ChannelSMS, ChannelEmail, ChannelPush}

func (c Channel) Valid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelPush:
		return true
	}
	return false
}

// Priority bildirimin işlenme önceliğidir.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}

// Status bildirimin yaşam döngüsündeki durumudur.
type Status string

const (
	StatusPending    Status = "pending"
	StatusScheduled  Status = "scheduled"
	StatusProcessing Status = "processing"
	StatusSent       Status = "sent"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusScheduled, StatusProcessing, StatusSent, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Cancellable: yalnızca henüz işlenmeye başlamamış bildirimler iptal edilebilir.
func (s Status) Cancellable() bool {
	return s == StatusPending || s == StatusScheduled
}

// Notification veritabanındaki bir bildirim kaydını temsil eder.
// Boş olabilen (NULL) kolonlar pointer olarak tutulur.
type Notification struct {
	ID                uuid.UUID  `json:"id"`
	BatchID           *uuid.UUID `json:"batch_id,omitempty"`
	IdempotencyKey    *string    `json:"idempotency_key,omitempty"`
	Recipient         string     `json:"recipient"`
	Channel           Channel    `json:"channel"`
	Content           string     `json:"content"`
	Priority          Priority   `json:"priority"`
	Status            Status     `json:"status"`
	Attempts          int        `json:"attempts"`
	LastError         *string    `json:"last_error,omitempty"`
	ProviderMessageID *string    `json:"provider_message_id,omitempty"`
	ScheduledAt       *time.Time `json:"scheduled_at,omitempty"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Traceparent bildirimin oluşturulduğu isteğin W3C iz bağlamıdır (API yanıtına girmez). Kuyruk, relay ve yeniden
	// denemeler boyunca aynı izin sürdürülmesini sağlar.
	Traceparent string `json:"-"`
}
