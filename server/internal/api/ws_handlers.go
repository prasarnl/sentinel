package api

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Dashboard is same-origin (served by this process); allow any origin
	// during local/dev use behind a reverse proxy.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// StreamHost upgrades to a websocket and pushes every ingest event for the
// given host as it happens, so the dashboard updates live without polling.
func (s *Server) StreamHost(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "id")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ch := s.Hub.Subscribe(hostID)
	defer s.Hub.Unsubscribe(hostID, ch)

	// Detect client disconnects so we stop blocking on Publish.
	go func() {
		for {
			if _, _, err := conn.NextReader(); err != nil {
				conn.Close()
				return
			}
		}
	}()

	for msg := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}
