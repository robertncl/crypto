package ws

import (
	"encoding/json"
	"testing"
	"time"
)

func newTestClient() *Client {
	return &Client{send: make(chan []byte, 8)}
}

func TestHubSubscribeAndPublish(t *testing.T) {
	h := NewHub()
	c := newTestClient()
	h.subscribe(c, "trades:BTC-USDT")
	h.Publish("trades:BTC-USDT", map[string]string{"hello": "world"})

	select {
	case msg := <-c.send:
		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Channel != "trades:BTC-USDT" {
			t.Errorf("channel = %q, want trades:BTC-USDT", env.Channel)
		}
	default:
		t.Fatal("expected a published message, got none")
	}
}

func TestHubPublishNoSubscribers(t *testing.T) {
	h := NewHub()
	h.Publish("nobody-listening", "data") // must not panic
}

func TestHubPublishOnlyToSubscribers(t *testing.T) {
	h := NewHub()
	a := newTestClient()
	b := newTestClient()
	h.subscribe(a, "topic-a")
	h.subscribe(b, "topic-b")
	h.Publish("topic-a", "x")

	if len(a.send) != 1 {
		t.Errorf("subscriber A should have 1 message, got %d", len(a.send))
	}
	if len(b.send) != 0 {
		t.Errorf("non-subscriber B should have 0 messages, got %d", len(b.send))
	}
}

func TestHubUnsubscribe(t *testing.T) {
	h := NewHub()
	c := newTestClient()
	h.subscribe(c, "topic")
	h.unsubscribe(c, "topic")
	h.Publish("topic", "data")

	if len(c.send) != 0 {
		t.Error("should not receive messages after unsubscribe")
	}
	// Topic should be cleaned up once empty.
	h.mu.RLock()
	_, exists := h.topics["topic"]
	h.mu.RUnlock()
	if exists {
		t.Error("empty topic should be deleted")
	}
}

func TestHubRemove(t *testing.T) {
	h := NewHub()
	c := newTestClient()
	h.subscribe(c, "a")
	h.subscribe(c, "b")
	h.remove(c)
	h.Publish("a", "data")
	h.Publish("b", "data")

	if len(c.send) != 0 {
		t.Error("should not receive after remove")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.topics) != 0 {
		t.Errorf("topics not cleaned up after remove: %d remain", len(h.topics))
	}
}

func TestHubPublishDoesNotBlockOnSlowClient(t *testing.T) {
	h := NewHub()
	c := &Client{send: make(chan []byte)} // unbuffered, never read
	h.subscribe(c, "topic")

	done := make(chan struct{})
	go func() {
		h.Publish("topic", "data") // full buffer → drop, must not block
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow client")
	}
}

func TestResolveTopicPublic(t *testing.T) {
	c := &Client{userID: 0}
	topic, ok := c.resolveTopic("trades")
	if !ok || topic != "trades" {
		t.Errorf("resolveTopic(trades) = %q,%v, want trades,true", topic, ok)
	}
}

func TestResolveTopicPrivateUnauthenticated(t *testing.T) {
	c := &Client{userID: 0}
	if _, ok := c.resolveTopic("orders"); ok {
		t.Error("unauthenticated client must not resolve a private channel")
	}
}

func TestResolveTopicPrivateAuthenticated(t *testing.T) {
	c := &Client{userID: 42}
	topic, ok := c.resolveTopic("orders")
	if !ok || topic != "orders:42" {
		t.Errorf("resolveTopic(orders) = %q,%v, want orders:42,true", topic, ok)
	}
}
