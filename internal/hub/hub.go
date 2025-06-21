package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	WriteWait  = 10 * time.Second
	PongWait   = 60 * time.Second
	PingPeriod = (PongWait * 9) / 10
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	Msgs    chan []byte
}

type client struct {
	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	filter *fieldFilter
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

type fieldFilter struct {
	include  bool
	children map[string]*fieldFilter
}

func newFieldFilter() *fieldFilter {
	return &fieldFilter{
		children: make(map[string]*fieldFilter),
	}
}

func (f *fieldFilter) addPath(path string) {
	parts := strings.Split(path, ".")
	current := f

	for i, part := range parts {
		if current.children[part] == nil {
			current.children[part] = newFieldFilter()
		}
		current = current.children[part]

		if i == len(parts)-1 {
			current.include = true
		}
	}
}

func (f *fieldFilter) shouldInclude(path []string) bool {
	current := f

	for i, part := range path {
		child, exists := current.children[part]
		if !exists {
			return false
		}

		if child.include {
			return true
		}

		if i == len(path)-1 && len(child.children) > 0 {
			return true
		}

		current = child
	}

	return current.include
}

var allowedFields = map[string]bool{
	"cert_pem":         true,
	"cert_fingerprint": true,
	"subject":          true,
	"sans":             true,
	"issuer":           true,
	"not_before":       true,
	"not_after":        true,
	"source":           true,
	"timestamp":        true,

	"subject.CN":  true,
	"subject.O":   true,
	"subject.OU":  true,
	"subject.C":   true,
	"subject.raw": true,
	"issuer.CN":   true,
	"issuer.O":    true,
	"issuer.OU":   true,
	"issuer.C":    true,
	"issuer.raw":  true,

	"sans.dns_names":    true,
	"sans.ip_addresses": true,

	"cert_fingerprint.sha256": true,
}

func New() *Hub {
	return &Hub{
		clients: make(map[*client]struct{}),
		Msgs:    make(chan []byte, 1024),
	}
}

func (h *Hub) Run() {
	for msg := range h.Msgs {
		h.mu.RLock()
		for c := range h.clients {

			filteredMsg := msg
			if c.filter != nil {
				filtered, err := filterMessage(msg, c.filter)
				if err != nil {
					log.Printf("filter error: %v", err)
					continue
				}
				filteredMsg = filtered
			}

			select {
			case c.send <- filteredMsg:
			default:
				h.removeClient(c)
			}
		}
		h.mu.RUnlock()
	}
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}

	filter := parseFilter(r.URL.Query().Get("filter"))

	c := &client{
		conn:   conn,
		send:   make(chan []byte, 256),
		hub:    h,
		filter: filter,
	}

	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go c.write()
	go c.read()
}

func (h *Hub) removeClient(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (c *client) read() {
	defer func() {
		c.hub.removeClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (c *client) write() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func parseFilter(filterParam string) *fieldFilter {
	if filterParam == "" {
		return nil
	}

	filter := newFieldFilter()
	fields := strings.Split(filterParam, ",")
	hasValidFields := false

	for _, field := range fields {
		field = strings.TrimSpace(field)
		if allowedFields[field] {
			filter.addPath(field)
			hasValidFields = true
		}
	}

	if !hasValidFields {
		return nil
	}

	return filter
}

func filterMessage(msg []byte, filter *fieldFilter) ([]byte, error) {

	var data map[string]interface{}
	if err := json.Unmarshal(msg, &data); err != nil {
		return nil, err
	}

	filtered := filterObject(data, filter, []string{})

	return json.Marshal(filtered)
}

func filterObject(obj map[string]interface{}, filter *fieldFilter, path []string) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range obj {
		currentPath := append(path, key)

		if !filter.shouldInclude(currentPath) {
			continue
		}

		if nestedObj, ok := value.(map[string]interface{}); ok {

			fieldFilter := filter
			for _, p := range currentPath {
				if fieldFilter.children[p] != nil {
					fieldFilter = fieldFilter.children[p]
				}
			}

			if len(fieldFilter.children) > 0 {
				filtered := filterObject(nestedObj, filter, currentPath)
				if len(filtered) > 0 {
					result[key] = filtered
				}
			} else {

				result[key] = value
			}
		} else {

			result[key] = value
		}
	}

	return result
}

func (h *Hub) NeedsCertData() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {

		if c.filter == nil {
			return true
		}

		if c.filter.shouldInclude([]string{"cert_pem"}) ||
			c.filter.shouldInclude([]string{"cert_fingerprint"}) {
			return true
		}
	}
	return false
}
