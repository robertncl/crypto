package ws

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
	sendBuffer     = 256
)

// privateChannels are user-scoped; the hub topic is suffixed with the client's
// user id so users only ever receive their own private stream.
var privateChannels = map[string]bool{
	"orders":     true,
	"balances":   true,
	"walletTxns": true,
	"perpOrders": true,
	"positions":  true,
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
// client may not subscribe (e.g. private channel while unauthenticated).
func (c *Client) resolveTopic(channel string) (string, bool) {
	if privateChannels[channel] {
		if c.userID == 0 {
			return "", false
		}
		return channel + ":" + strconv.FormatInt(c.userID, 10), true
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
				c.hub.subscribe(c, topic)
			case "unsubscribe":
				c.hub.unsubscribe(c, topic)
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
