package accountpriority

import "testing"

func TestClamp(t *testing.T) {
	tests := []struct {
		value int
		want  int
	}{
		{value: Min - 1, want: Min},
		{value: Min, want: Min},
		{value: 0, want: 0},
		{value: Max, want: Max},
		{value: Max + 1, want: Max},
	}
	for _, tt := range tests {
		if got := Clamp(tt.value); got != tt.want {
			t.Fatalf("Clamp(%d) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestAddOffset(t *testing.T) {
	if got, ok := AddOffset(50, -75); !ok || got != -25 {
		t.Fatalf("AddOffset(50, -75) = %d, %v, want -25, true", got, ok)
	}
	if _, ok := AddOffset(Max, 1); ok {
		t.Fatal("AddOffset(Max, 1) should reject an out-of-range result")
	}
	if _, ok := AddOffset(Min, -1); ok {
		t.Fatal("AddOffset(Min, -1) should reject an out-of-range result")
	}
}
