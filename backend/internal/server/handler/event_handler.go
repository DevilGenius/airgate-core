package handler

import "github.com/DevilGenius/airgate-core/internal/adminevents"

// EventHandler handles admin server event streams.
type EventHandler struct {
	hub *adminevents.Hub
}

// NewEventHandler creates an EventHandler.
func NewEventHandler(hub *adminevents.Hub) *EventHandler {
	return &EventHandler{hub: hub}
}
