package plugin

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type requestBodyFillReader struct{}

func (requestBodyFillReader) Read(p []byte) (int, error) { return len(p), nil }

func TestMultimodalRequestBodyBoundary(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/responses/compact", "/v1/chat/completions", "/v1/messages", "/v1/images/edits"} {
		for _, size := range []int64{96 << 20, (96 << 20) + 1} {
			// Exercise chunked bodies too: admission must not depend on Content-Length.
			body := io.NopCloser(io.LimitReader(requestBodyFillReader{}, size))
			limited := http.MaxBytesReader(httptest.NewRecorder(), body, gatewayBodyLimit(path, "application/json"))
			_, err := io.Copy(io.Discard, limited)
			_ = limited.Close()
			var tooLarge *http.MaxBytesError
			if size == 96<<20 && err != nil {
				t.Fatalf("%s rejected 96 MiB: %v", path, err)
			}
			if size > 96<<20 && !errors.As(err, &tooLarge) {
				t.Fatalf("%s accepted oversized body: %v", path, err)
			}
		}
	}
}

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
