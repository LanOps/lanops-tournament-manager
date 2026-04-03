package handlers

import (
	"fmt"
	"net/http"
	"sync"
)

// BracketBroker manages SSE connections for a single tournament's bracket.
type BracketBroker struct {
	clients   map[chan []byte]struct{}
	mu        sync.Mutex
	join      chan chan []byte
	leave     chan chan []byte
	broadcast chan []byte
	done      chan struct{}
}

func newBracketBroker() *BracketBroker {
	b := &BracketBroker{
		clients:   make(map[chan []byte]struct{}),
		join:      make(chan chan []byte),
		leave:     make(chan chan []byte),
		broadcast: make(chan []byte, 16),
		done:      make(chan struct{}),
	}
	go b.run()
	return b
}

func (b *BracketBroker) run() {
	for {
		select {
		case ch := <-b.join:
			b.mu.Lock()
			b.clients[ch] = struct{}{}
			b.mu.Unlock()
		case ch := <-b.leave:
			b.mu.Lock()
			delete(b.clients, ch)
			b.mu.Unlock()
			close(ch)
		case msg := <-b.broadcast:
			b.mu.Lock()
			for ch := range b.clients {
				select {
				case ch <- msg:
				default:
					// slow consumer: drop message, never block
				}
			}
			b.mu.Unlock()
		case <-b.done:
			return
		}
	}
}

func (b *BracketBroker) Subscribe() chan []byte {
	ch := make(chan []byte, 4)
	b.join <- ch
	return ch
}

func (b *BracketBroker) Unsubscribe(ch chan []byte) {
	b.leave <- ch
}

func (b *BracketBroker) Send(msg []byte) {
	select {
	case b.broadcast <- msg:
	default:
	}
}

func (b *BracketBroker) Close() {
	close(b.done)
}

func (b *BracketBroker) ClientCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.clients)
}

// BracketBrokerMap manages one BracketBroker per tournament.
type BracketBrokerMap struct {
	mu      sync.Mutex
	brokers map[int64]*BracketBroker
}

func NewBracketBrokerMap() *BracketBrokerMap {
	return &BracketBrokerMap{
		brokers: make(map[int64]*BracketBroker),
	}
}

func (m *BracketBrokerMap) GetOrCreate(tournamentID int64) *BracketBroker {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.brokers[tournamentID]; ok {
		return b
	}
	b := newBracketBroker()
	m.brokers[tournamentID] = b
	return b
}

func (m *BracketBrokerMap) Get(tournamentID int64) (*BracketBroker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.brokers[tournamentID]
	return b, ok
}

// BroadcastBracketUpdate sends a reload signal to all SSE clients watching this tournament.
func (m *BracketBrokerMap) BroadcastBracketUpdate(tournamentID int64) {
	b, ok := m.Get(tournamentID)
	if !ok {
		return
	}
	msg := []byte(fmt.Sprintf("event: bracket-update\ndata: %d\n\n", tournamentID))
	b.Send(msg)
}

// SSEHandler handles GET /tournaments/{id}/events
func SSEHandler(brokers *BracketBrokerMap) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tournamentID, err := parseIDParam(r, "id")
		if err != nil {
			http.Error(w, "invalid tournament id", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		broker := brokers.GetOrCreate(tournamentID)
		ch := broker.Subscribe()
		defer broker.Unsubscribe(ch)

		// Send initial keep-alive comment
		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				w.Write(msg)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
