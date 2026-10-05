package adminevents

import "context"

// Service owns the broadcast hub and coalescing publisher under one lifecycle.
type Service struct {
	*AccountChangePublisher
	hub *Hub
}

func NewService(buffer int) *Service {
	hub := NewHub(buffer)
	return &Service{hub: hub, AccountChangePublisher: NewAccountChangePublisher(hub)}
}

func (s *Service) PublishAccountChanged(id int, patch AccountPatch) {
	if s != nil {
		s.AccountChangePublisher.PublishAccountChanged(id, patch)
	}
}

func (s *Service) SubscribeWithSequence(ctx context.Context) (<-chan Event, func(), uint64) {
	if s == nil {
		return (*Hub)(nil).SubscribeWithSequence(ctx)
	}
	return s.hub.SubscribeWithSequence(ctx)
}

func (s *Service) PublishAccountCapacityChanged(id, count int) {
	if s != nil {
		s.hub.PublishAccountCapacityChanged(id, count)
	}
}

func (s *Service) BroadcastMonitorChanged(reason string) {
	if s != nil {
		s.hub.BroadcastMonitorChanged(reason)
	}
}
