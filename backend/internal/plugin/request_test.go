package plugin

import "testing"

func TestRequestNeedsImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		model string
		body  []byte
		want  bool
	}{
		{
			name:  "chat request",
			path:  "/v1/chat/completions",
			model: "gpt-4o",
			want:  false,
		},
		{
			name:  "image api path",
			path:  "/v1/images/generations",
			model: "gpt-4o",
			want:  true,
		},
		{
			name:  "image api path with query",
			path:  "/v1/images/edits?debug=1",
			model: "gpt-4o",
			want:  true,
		},
		{
			name:  "image task query is metadata",
			path:  "/v1/images/tasks",
			model: "",
			want:  false,
		},
		{
			name:  "image task list is metadata",
			path:  "/v1/images/tasks/list",
			model: "",
			want:  false,
		},
		{
			name:  "image model",
			path:  "/v1/responses",
			model: "gpt-image-2",
			want:  true,
		},
		{
			name:  "responses image tool",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}]}`),
			want:  false,
		},
		{
			name:  "responses other tool",
			path:  "/v1/responses",
			model: "gpt-5.4",
			body:  []byte(`{"model":"gpt-5.4","tools":[{"type":"web_search"}]}`),
			want:  false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := requestNeedsImage(tt.path, tt.model, tt.body); got != tt.want {
				t.Fatalf("requestNeedsImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMetadataOnlyPathNormalizesPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "/v1/models", want: true},
		{path: "/v1/images/tasks?task_id=abc", want: true},
		{path: "/v1/images/tasks/", want: true},
		{path: "/v1/images/tasks/list?limit=10", want: true},
		{path: "/v1/images/generations", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := isMetadataOnlyPath(tt.path); got != tt.want {
				t.Fatalf("isMetadataOnlyPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
