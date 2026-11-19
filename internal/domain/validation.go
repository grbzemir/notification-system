package domain

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Kanal başına içerik karakter limitleri (rune sayısı olarak).
var maxContentLength = map[Channel]int{
	ChannelSMS:   160,
	ChannelPush:  256,
	ChannelEmail: 10000,
}

const maxPushTokenLength = 4096

// E.164: "+" ile başlayan, 8-15 haneli uluslararası telefon formatı.
var e164 = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

// NewNotification yeni bir bildirim oluşturma isteğinin doğrulanmamış halidir.
type NewNotification struct {
	Recipient string
	Channel   Channel
	Content   string
	Priority  Priority
}

// FieldError tek bir alandaki doğrulama hatasıdır.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError bir veya daha fazla alan hatasını taşır.
type ValidationError struct {
	Errors []FieldError
}

func (v *ValidationError) Error() string {
	parts := make([]string, len(v.Errors))
	for i, e := range v.Errors {
		parts[i] = e.Field + ": " + e.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Validate alanları kontrol eder. Öncelik boşsa "normal" olarak atanır.
// Hata varsa tüm alan hatalarını tek seferde *ValidationError olarak döner.
func (n *NewNotification) Validate() error {
	if n.Priority == "" {
		n.Priority = PriorityNormal
	}

	var errs []FieldError
	add := func(field, msg string) { errs = append(errs, FieldError{field, msg}) }

	if !n.Channel.Valid() {
		add("channel", "must be one of: sms, email, push")
	}
	if !n.Priority.Valid() {
		add("priority", "must be one of: high, normal, low")
	}

	if strings.TrimSpace(n.Recipient) == "" {
		add("recipient", "is required")
	} else if n.Channel.Valid() {
		if msg := validateRecipient(n.Channel, n.Recipient); msg != "" {
			add("recipient", msg)
		}
	}

	if strings.TrimSpace(n.Content) == "" {
		add("content", "is required")
	} else if n.Channel.Valid() {
		if max := maxContentLength[n.Channel]; utf8.RuneCountInString(n.Content) > max {
			add("content", fmt.Sprintf("must be at most %d characters for channel %s", max, n.Channel))
		}
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

func validateRecipient(ch Channel, recipient string) string {
	switch ch {
	case ChannelSMS:
		if !e164.MatchString(recipient) {
			return "must be a phone number in E.164 format, e.g. +905551234567"
		}
	case ChannelEmail:
		addr, err := mail.ParseAddress(recipient)
		if err != nil || addr.Address != recipient {
			return "must be a valid email address"
		}
	case ChannelPush:
		if len(recipient) > maxPushTokenLength {
			return fmt.Sprintf("device token must be at most %d characters", maxPushTokenLength)
		}
	}
	return ""
}
