package forwardpath

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "absolute url with path",
			path: "https://example.com/v1/chat/completions?trace=1",
			want: "/v1/chat/completions",
		},
		{
			name: "relative path with query",
			path: "v1/chat/completions?trace=1",
			want: "/v1/chat/completions",
		},
		{
			name: "root path",
			path: "/",
			want: "/",
		},
		{
			name: "trailing slash stripped",
			path: "/v1/chat/completions/",
			want: "/v1/chat/completions",
		},
		{
			name: "already clean",
			path: "/v1/chat/completions",
			want: "/v1/chat/completions",
		},
		{
			name: "mixed case",
			path: "/V1/Chat/Completions",
			want: "/v1/chat/completions",
		},
		{
			name: "relative path no query",
			path: "v1/chat/completions",
			want: "/v1/chat/completions",
		},
		{
			name: "absolute url without path strips query from original material",
			path: "https://example.com?trace=1",
			want: "/https://example.com",
		},
		{
			name: "absolute url root path",
			path: "https://example.com/",
			want: "/",
		},
		{
			name: "empty",
			path: " ",
			want: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Normalize(tt.path); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
