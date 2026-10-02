package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/DevilGenius/airgate-core/internal/adminevents"
)

// EventHandler handles admin server event streams.
type EventHandler struct {
	hub *adminevents.Hub
}

// NewEventHandler creates an EventHandler.
func NewEventHandler(hub *adminevents.Hub) *EventHandler {
	return &EventHandler{hub: hub}
}

// StreamCredentialEvents keeps a credential-management SSE connection open.
// It intentionally shares the in-memory admin event hub with the admin UI;
// authentication is provided by the credential route middleware. The stream
// itself never reads or writes the database.
func (h *EventHandler) StreamCredentialEvents(c *gin.Context) {
	h.streamEvents(c)
}
