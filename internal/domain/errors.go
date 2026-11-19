package domain

import "errors"

// Katmanlar arası paylaşılan hatalar. Handler bunları HTTP durum kodlarına çevirir.
var (
	ErrNotFound            = errors.New("notification not found")
	ErrNotCancellable      = errors.New("notification can no longer be cancelled")
	ErrDuplicateKey        = errors.New("idempotency key already used")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrTemplateNotFound    = errors.New("template not found")
	ErrTemplateNameTaken   = errors.New("a template with this name already exists")
)
