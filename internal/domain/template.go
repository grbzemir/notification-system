package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxTemplateNameLength = 100
	MaxTemplateBodyLength = 10000
)

// placeholder {{ad}} biçimindeki yer tutucuları yakalar; ad harf/rakam/alt çizgiden oluşur, süslü parantez içinde boşluk serbesttir.
var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Template yeniden kullanılabilir bir bildirim metnidir.
type Template struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	Variables []string  `json:"variables"` // gövdedeki yer tutucu adları (kaydedilmez, gövdeden türetilir)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewTemplate yeni bir şablon oluşturma isteğinin doğrulanmamış halidir.
type NewTemplate struct {
	Name string
	Body string
}

// ExtractVariables gövdedeki yer tutucu adlarını ilk görünme sırasıyla, tekrarsız döner.
func ExtractVariables(body string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range placeholder.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Validate ad ve gövdeyi kontrol eder; tüm alan hatalarını tek seferde *ValidationError olarak döner.
func (t NewTemplate) Validate() error {
	var errs []FieldError
	add := func(field, msg string) { errs = append(errs, FieldError{Field: field, Message: msg}) }

	name := strings.TrimSpace(t.Name)
	switch {
	case name == "":
		add("name", "is required")
	case utf8.RuneCountInString(name) > MaxTemplateNameLength:
		add("name", "must be at most 100 characters")
	}

	switch {
	case strings.TrimSpace(t.Body) == "":
		add("body", "is required")
	case utf8.RuneCountInString(t.Body) > MaxTemplateBodyLength:
		add("body", "must be at most 10000 characters")
	case strings.Count(t.Body, "{{") != len(placeholder.FindAllString(t.Body, -1)) ||
		strings.Count(t.Body, "}}") != len(placeholder.FindAllString(t.Body, -1)):
		add("body", "contains a malformed placeholder; use {{name}} with letters, digits and underscores")
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// Render yer tutucuları vars ile doldurur. Şablonun istediği bir değişken eksikse tüm eksikleri
// "variables.ad" alan hatalarıyla birlikte döner. Fazladan verilen değişkenler yok sayılır.
// Değerler tek geçişte yerleştirilir: bir değerin içindeki {{x}} yeniden yorumlanmaz.
func (t *Template) Render(vars map[string]string) (string, error) {
	var missing []FieldError
	for _, name := range ExtractVariables(t.Body) {
		if _, ok := vars[name]; !ok {
			missing = append(missing, FieldError{Field: "variables." + name, Message: "is required by the template"})
		}
	}
	if len(missing) > 0 {
		return "", &ValidationError{Errors: missing}
	}
	return placeholder.ReplaceAllStringFunc(t.Body, func(m string) string {
		return vars[placeholder.FindStringSubmatch(m)[1]]
	}), nil
}
