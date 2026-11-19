package handler

import "testing"

func TestValidIDsNormalizesAndRejects(t *testing.T) {
	got, ok := validIDs([]string{"3F6C1C52-8C37-4A0B-8B43-0D6C8D1A9E11"})
	if !ok || len(got) != 1 || got[0] != "3f6c1c52-8c37-4a0b-8b43-0d6c8d1a9e11" {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := validIDs([]string{"not-a-uuid"}); ok {
		t.Fatal("invalid id must be rejected")
	}
	if got, ok := validIDs(nil); !ok || len(got) != 0 {
		t.Fatalf("empty list is valid: %v %v", got, ok)
	}
}
