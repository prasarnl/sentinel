package ws

import "sync"

// Hub fans out ingest events to subscribed dashboard clients, keyed by host ID.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan []byte]struct{})}
}

func (h *Hub) Subscribe(hostID string) chan []byte {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[hostID] == nil {
		h.subs[hostID] = make(map[chan []byte]struct{})
	}
	h.subs[hostID][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(hostID string, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.subs[hostID]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(h.subs, hostID)
		}
	}
	close(ch)
}

func (h *Hub) Publish(hostID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[hostID] {
		select {
		case ch <- payload:
		default:
			// slow consumer, drop the message rather than block ingest
		}
	}
}
