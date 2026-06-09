package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const adminEventsPingInterval = 15 * time.Second

// StreamAdminEvents keeps an authenticated admin SSE connection open.
func (h *EventHandler) StreamAdminEvents(c *gin.Context) {
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte("retry: 5000\n\n"))
	sendSSEEvent(w, gin.H{
		"type": "connected",
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
	})

	events, unsubscribe := h.hub.Subscribe(c.Request.Context())
	defer unsubscribe()

	ticker := time.NewTicker(adminEventsPingInterval)
	defer ticker.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			sendSSEEvent(w, event)
		case t := <-ticker.C:
			sendSSEEvent(w, gin.H{
				"type": "ping",
				"ts":   t.UTC().Format(time.RFC3339Nano),
			})
		}
	}
}
