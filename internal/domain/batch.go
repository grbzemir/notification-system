package domain

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	// MaxBatchSize tek istekte oluşturulabilecek en fazla bildirim sayısıdır.
	MaxBatchSize = 1000
	// maxReportedErrors çok büyük bir batch'te cevabın şişmemesi için raporlanan hata sayısını sınırlar.
	maxReportedErrors = 100
)

// ValidateBatch tüm öğeleri doğrular. Hata alan adları öğe sırasını içerir: "notifications[3].channel".
// Bir öğe bile geçersizse batch'in tamamı reddedilir (hepsi ya da hiçbiri).
func ValidateBatch(items []NewNotification) error {
	if len(items) == 0 {
		return &ValidationError{Errors: []FieldError{{Field: "notifications", Message: "must contain at least 1 item"}}}
	}
	if len(items) > MaxBatchSize {
		return &ValidationError{Errors: []FieldError{{
			Field: "notifications", Message: fmt.Sprintf("must contain at most %d items", MaxBatchSize),
		}}}
	}

	var errs []FieldError
	for i := range items {
		err := items[i].Validate() // pointer receiver: boş öncelik burada "normal" olur
		if err == nil {
			continue
		}
		var verr *ValidationError
		if errors.As(err, &verr) {
			for _, fe := range verr.Errors {
				errs = append(errs, FieldError{Field: fmt.Sprintf("notifications[%d].%s", i, fe.Field), Message: fe.Message})
			}
		}
		if len(errs) >= maxReportedErrors {
			break
		}
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// BatchSummary bir batch'teki bildirimlerin durum dağılımıdır.
type BatchSummary struct {
	BatchID      uuid.UUID      `json:"batch_id"`
	Total        int            `json:"total"`
	StatusCounts map[Status]int `json:"status_counts"`
}
