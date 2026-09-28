package traceredaction

import (
	"encoding/json"
	"strings"
	"testing"
)

func BenchmarkSanitizeBody(b *testing.B) {
	for _, tc := range []struct {
		name  string
		size  int
		field string
	}{
		{"plain_64KiB", 64 << 10, ""}, {"plain_1MiB", 1 << 20, ""}, {"credentials_64KiB", 64 << 10, "access_token"}, {"image_64KiB", 64 << 10, "partial_image_b64"},
	} {
		items := make([]any, 64)
		for i := range items {
			items[i] = map[string]any{"role": "user", "content": strings.Repeat("x", tc.size/64)}
		}
		input := map[string]any{"model": "fixture", "input": items}
		if tc.field != "" {
			input[tc.field] = strings.Repeat("a", 4096)
		}
		body, err := json.Marshal(input)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				result := SanitizeBody(body, "application/json", BodyOptions{})
				if len(result.Body) == 0 {
					b.Fatal("empty output")
				}
			}
		})
	}
}
