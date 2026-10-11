// Package ws implements a topic-based WebSocket fan-out hub. Producers (the
// matching engine, market-data service, wallet) call Publish; connected clients
// subscribe to topics and receive JSON envelopes.
package ws

import (
	"encoding/json"
	"sync"
)

// Envelope is the wire format pushed to clients: a channel name plus a payload.
type Envelope struct {
	Channel string `json:"channel"`
	Data    any    `json:"data"`
}

// Hub tracks subscriptions and routes published messages to interested clients.
type Hub struct {
	mu     sync.RWMutex
	topics map[string]map[*Client]struct{} // topic -> set of clients
}

func NewHub() *Hub {
	return &Hub{topics: map[string]map[*Client]struct{}{}}
}

func (h *Hub) subscribe(c *Client, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.topics[topic]
	if set == nil {
		set = map[*Client]struct{}{}
		h.topics[topic] = set
	}
	set[c] = struct{}{}
}

func (h *Hub) unsubscribe(c *Client, topic string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.topics[topic]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.topics, topic)
		}
	}
}

// remove drops a client from the given topics (its own subscriptions, called
// on disconnect). Cost is proportional to the client's subscriptions, not to
// the total number of topics on the hub.
func (h *Hub) remove(c *Client, topics map[string]struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for topic := range topics {
		if set := h.topics[topic]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.topics, topic)
			}
		}
	}
}

// HasSubscribers reports whether any client is subscribed to topic. Producers
// use it to skip building payloads (e.g. a balance query) nobody will receive.
func (h *Hub) HasSubscribers(topic string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic]) > 0
}

// Publish encodes data once and delivers it to every client subscribed to topic.
// Slow clients whose buffers are full are skipped (their connection will be
// reaped by the write pump), so a stalled consumer can never block producers.
// Topics with no subscribers return before any encoding work.
func (h *Hub) Publish(topic string, data any) {
	h.mu.RLock()
	set := h.topics[topic]
	if len(set) == 0 {
		h.mu.RUnlock()
		return
	}
	clients := make([]*Client, 0, len(set))
	for c := range set {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	payload, err := json.Marshal(Envelope{Channel: topic, Data: data})
	if err != nil {
		return
	}
	for _, c := range clients {
		select {
		case c.send <- payload:
		default:
			// Buffer full: drop this message for the slow client.
		}
	}
}
