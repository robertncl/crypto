package api

import (
	"net/http"
	"net/url"
	"strings"

	"cryptoex/internal/ws"

	"github.com/gorilla/websocket"
)

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
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.checkWSOrigin,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.Serve(s.hub, conn, userID)
}

// checkWSOrigin admits same-origin browsers and the configured SPA origins.
// Streams authenticate with a token rather than cookies, so this is not a CSRF
// guard; it stops third-party pages from opening sockets through their
// visitors' browsers. Clients that send no Origin (non-browser) are allowed.
func (s *Server) checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	for _, o := range s.origins {
		if strings.EqualFold(origin, o) {
			return true
		}
	}
	return false
}
