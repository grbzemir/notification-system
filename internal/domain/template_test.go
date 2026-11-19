package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestExtractVariables(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"none", "merhaba dünya", []string{}},
		{"single", "Merhaba {{name}}", []string{"name"}},
		{"order and dedupe", "{{b}} {{a}} {{b}}", []string{"b", "a"}},
		{"inner spaces", "Kod: {{ code }}", []string{"code"}},
		{"underscore and digits", "{{user_1}}", []string{"user_1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractVariables(tt.body); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTemplateValidate(t *testing.T) {
	tests := []struct {
		name      string
		in        NewTemplate
		wantField []string // boş: geçerli
	}{
		{"valid", NewTemplate{Name: "otp", Body: "Kod {{code}}"}, nil},
		{"valid without placeholders", NewTemplate{Name: "x", Body: "sabit metin"}, nil},
		{"empty", NewTemplate{}, []string{"name", "body"}},
		{"blank name", NewTemplate{Name: "   ", Body: "x"}, []string{"name"}},
		{"long name", NewTemplate{Name: strings.Repeat("a", 101), Body: "x"}, []string{"name"}},
		{"long body", NewTemplate{Name: "n", Body: strings.Repeat("a", 10001)}, []string{"body"}},
		{"unclosed placeholder", NewTemplate{Name: "n", Body: "Merhaba {{name"}, []string{"body"}},
		{"bad placeholder name", NewTemplate{Name: "n", Body: "Merhaba {{1x}}"}, []string{"body"}},
		{"stray closing", NewTemplate{Name: "n", Body: "Merhaba name}}"}, []string{"body"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantField == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("want *ValidationError, got %v", err)
			}
			var got []string
			for _, fe := range verr.Errors {
				got = append(got, fe.Field)
			}
			if !reflect.DeepEqual(got, tt.wantField) {
				t.Fatalf("fields = %v, want %v", got, tt.wantField)
			}
		})
	}
}

func TestTemplateRender(t *testing.T) {
	tpl := &Template{Body: "Merhaba {{name}}, kodunuz {{ code }}. İyi günler {{name}}!"}

	got, err := tpl.Render(map[string]string{"name": "Ada", "code": "42", "fazla": "yok sayılır"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Merhaba Ada, kodunuz 42. İyi günler Ada!"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateRenderMissingVariables(t *testing.T) {
	tpl := &Template{Body: "{{a}} {{b}} {{c}}"}

	_, err := tpl.Render(map[string]string{"b": "x"})

	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	var got []string
	for _, fe := range verr.Errors {
		got = append(got, fe.Field)
	}
	if want := []string{"variables.a", "variables.c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %v, want %v", got, want)
	}
}

// Bir değerin içindeki {{x}} yeniden yorumlanmamalı (şablon enjeksiyonu).
func TestTemplateRenderDoesNotReinterpretValues(t *testing.T) {
	tpl := &Template{Body: "Selam {{name}} {{other}}"}

	got, err := tpl.Render(map[string]string{"name": "{{other}}", "other": "GİZLİ"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Selam {{other}} GİZLİ"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTemplateRenderEmptyValueIsAllowed(t *testing.T) {
	tpl := &Template{Body: "[{{x}}]"}
	got, err := tpl.Render(map[string]string{"x": ""})
	if err != nil || got != "[]" {
		t.Fatalf("got %q, %v", got, err)
	}
}
