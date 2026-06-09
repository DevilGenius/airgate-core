package handler

import (
	"encoding/json"
	"net/http"
)

func sendSSEEvent(w http.ResponseWriter, data any) {
	body, _ := json.Marshal(data)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
