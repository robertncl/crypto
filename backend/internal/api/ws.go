package api

import (
	"net/http"

	"cryptoex/internal/ws"

	"github.com/gorilla/websocket"
)

// upgrader promotes HTTP connections to WebSocket. Origin checks are relaxed
// because the API authenticates streams via the token query param, not cookies,
// so there is no CSRF surface to protect here.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}

// handleWS upgrades the connection and starts the client pumps. A valid token
// query param scopes the connection to a user for private channels; without one
// the client may still subscribe to public market-data channels.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if token := r.URL.Query().Get("token"); token != "" {
		if id, _, err := s.auth.Parse(token); err == nil {
			userID = id
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.Serve(s.hub, conn, userID)
}
