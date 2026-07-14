package plugin

import "testing"

func TestGatewayBodyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		path        string
		contentType string
		want        int64
	}{
		{name: "responses", path: "/v1/responses", want: largeGatewayBodyLimit},
		{name: "responses alias", path: "/responses", want: largeGatewayBodyLimit},
		{name: "responses compact", path: "/v1/responses/compact", want: largeGatewayBodyLimit},
		{name: "responses compact alias", path: "/responses/compact", want: largeGatewayBodyLimit},
		{name: "chat completions", path: "/v1/chat/completions", want: largeGatewayBodyLimit},
		{name: "chat completions alias", path: "/chat/completions", want: largeGatewayBodyLimit},
		{name: "messages", path: "/v1/messages", want: largeGatewayBodyLimit},
		{name: "messages alias", path: "/messages", want: largeGatewayBodyLimit},
		{name: "count tokens", path: "/v1/messages/count_tokens", want: largeGatewayBodyLimit},
		{name: "count tokens alias", path: "/messages/count_tokens", want: largeGatewayBodyLimit},
		{name: "normalized responses", path: "/V1/Responses/?trace=1", want: largeGatewayBodyLimit},
		{name: "image generation", path: "/v1/images/generations", want: largeGatewayBodyLimit},
		{name: "multipart", path: "/v1/other", contentType: "multipart/form-data; boundary=test", want: largeGatewayBodyLimit},
		{name: "default JSON", path: "/v1/embeddings", contentType: "application/json", want: defaultGatewayBodyLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := gatewayBodyLimit(tt.path, tt.contentType); got != tt.want {
				t.Fatalf("gatewayBodyLimit(%q, %q) = %d, want %d", tt.path, tt.contentType, got, tt.want)
			}
		})
	}
}
