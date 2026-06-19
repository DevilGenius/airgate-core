package plugin

import "testing"

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
