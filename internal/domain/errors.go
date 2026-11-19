package domain

import "errors"

// Katmanlar arası paylaşılan hatalar. Handler bunları HTTP durum kodlarına çevirir.
var (
	ErrNotFound       = errors.New("notification not found")
	ErrNotCancellable = errors.New("notification can no longer be cancelled")
	ErrDuplicateKey   = errors.New("idempotency key already used")
)
