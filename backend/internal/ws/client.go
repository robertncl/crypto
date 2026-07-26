package ws

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
	sendBuffer     = 256
	// maxSubscriptions caps how many topics one connection may hold, bounding
	// per-client memory so a single socket cannot exhaust the hub.
	maxSubscriptions = 256
)

// privateChannels are user-scoped; the hub topic is suffixed with the client's
// user id so users only ever receive their own private stream.
var privateChannels = map[string]bool{
	"orders":        true,
	"balances":      true,
	"walletTxns":    true,
	"perpOrders":    true,
	"positions":     true,
	"earnPositions": true,
}

// Client is a single WebSocket connection.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID int64 // 0 if unauthenticated
}

// clientMsg is an inbound control message.
type clientMsg struct {
	Op       string   `json:"op"` // subscribe | unsubscribe
	Channels []string `json:"channels"`
}

// Serve registers a new client and runs its read/write pumps until disconnect.
func Serve(hub *Hub, conn *websocket.Conn, userID int64) {
	c := &Client{hub: hub, conn: conn, send: make(chan []byte, sendBuffer), userID: userID}
	go c.writePump()
	c.readPump()
}

// resolveTopic maps a client-facing channel name to an internal hub topic,
// scoping private channels to this client's user id. Returns ("", false) if the
// client may not subscribe.
//
// A private channel may only be requested by its bare name (e.g. "balances");
// the server then appends the authenticated user id to form the hub topic
// ("balances:<uid>"). A client must never be able to name a user-scoped topic
// directly: subscribing to "balances:42" would otherwise route another user's
// private stream to this connection. So any channel whose prefix (before the
// first ':') is a private channel is rejected outright, authenticated or not.
func (c *Client) resolveTopic(channel string) (string, bool) {
	if privateChannels[channel] {
		if c.userID == 0 {
			return "", false
		}
		return channel + ":" + strconv.FormatInt(c.userID, 10), true
	}
	if prefix, _, found := strings.Cut(channel, ":"); found && privateChannels[prefix] {
		return "", false // attempt to name a user-scoped private topic directly
	}
	return channel, true
}

func (c *Client) readPump() {
	defer func() {
		c.hub.remove(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	// Track this connection's active topics so we can enforce a per-client cap.
	// Only this goroutine touches it, so no lock is needed.
	subscribed := map[string]struct{}{}
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg clientMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		for _, ch := range msg.Channels {
			topic, ok := c.resolveTopic(ch)
			if !ok {
				continue
			}
			switch msg.Op {
			case "subscribe":
				if _, already := subscribed[topic]; !already && len(subscribed) >= maxSubscriptions {
					continue // refuse to exceed the per-connection cap
				}
				c.hub.subscribe(c, topic)
				subscribed[topic] = struct{}{}
			case "unsubscribe":
				c.hub.unsubscribe(c, topic)
				delete(subscribed, topic)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
