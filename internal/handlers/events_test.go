package handlers_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/th0rn0/thournament/internal/handlers"
)

// TestBracketBroker_SendReceive verifies that a subscribed client receives broadcasted messages.
func TestBracketBroker_SendReceive(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	broker := brokers.GetOrCreate(1)

	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	msg := []byte("event: test\ndata: hello\n\n")
	broker.Send(msg)

	select {
	case got := <-ch:
		assert.Equal(t, msg, got)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for message")
	}
}

// TestBracketBroker_SlowConsumerDrops verifies that a slow consumer does not block fast ones.
func TestBracketBroker_SlowConsumerDrops(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	broker := brokers.GetOrCreate(2)

	// Subscribe a fast consumer and a blocked consumer
	fast := broker.Subscribe()
	slow := broker.Subscribe()
	defer broker.Unsubscribe(fast)
	defer broker.Unsubscribe(slow)

	// Fill slow consumer's buffer so it's full
	for i := 0; i < cap(slow); i++ {
		slow <- []byte("fill")
	}

	// Send a new message — should not block and fast consumer should get it
	done := make(chan struct{})
	go func() {
		broker.Send([]byte("event: test\ndata: broadcast\n\n"))
		close(done)
	}()

	select {
	case <-done:
		// good — send did not block
	case <-time.After(500 * time.Millisecond):
		t.Fatal("broker.Send blocked on slow consumer")
	}

	select {
	case msg := <-fast:
		assert.Contains(t, string(msg), "broadcast")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fast consumer did not receive message")
	}
}

// TestBracketBroker_NoGoroutineLeak verifies that unsubscribing a client cleans up.
func TestBracketBroker_NoGoroutineLeak(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	broker := brokers.GetOrCreate(3)

	var wg sync.WaitGroup
	const clients = 10
	channels := make([]chan []byte, clients)

	for i := 0; i < clients; i++ {
		channels[i] = broker.Subscribe()
	}

	require.Eventually(t, func() bool {
		return broker.ClientCount() == clients
	}, time.Second, 10*time.Millisecond, "all clients should be subscribed")

	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			broker.Unsubscribe(channels[i])
		}(i)
	}
	wg.Wait()

	// Give run loop time to process
	require.Eventually(t, func() bool {
		return broker.ClientCount() == 0
	}, time.Second, 10*time.Millisecond, "all clients should be unsubscribed")
}

// TestBracketBroker_MultipleClients verifies all connected clients receive the broadcast.
func TestBracketBroker_MultipleClients(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	broker := brokers.GetOrCreate(4)

	const n = 5
	channels := make([]chan []byte, n)
	for i := range channels {
		channels[i] = broker.Subscribe()
	}
	defer func() {
		for _, ch := range channels {
			broker.Unsubscribe(ch)
		}
	}()

	msg := []byte("event: bracket-update\ndata: 4\n\n")
	broker.Send(msg)

	for i, ch := range channels {
		select {
		case got := <-ch:
			assert.Equal(t, msg, got, "client %d should receive message", i)
		case <-time.After(500 * time.Millisecond):
			t.Errorf("client %d did not receive message", i)
		}
	}
}

// TestSSEHandler_Connects verifies that the SSE endpoint establishes a connection
// and returns SSE headers.
func TestSSEHandler_Connects(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()

	r := chi.NewRouter()
	r.Get("/tournaments/{id}/events", handlers.SSEHandler(brokers))

	// Use a cancellable context so we can stop the handler cleanly before reading
	// the response recorder. Without this, the handler is still running when the
	// test reads headers/body → data race.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/tournaments/5/events", nil).WithContext(ctx)
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ServeHTTP(rw, req)
	}()

	// Wait for the broker to register our subscriber — that happens after
	// the handler has written headers and the initial keep-alive comment.
	require.Eventually(t, func() bool {
		b, ok := brokers.Get(5)
		return ok && b.ClientCount() == 1
	}, time.Second, 10*time.Millisecond, "SSE handler should subscribe a client")

	// Stop the handler and wait for it to return before touching rw.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	// Verify SSE headers
	assert.Equal(t, "text/event-stream", rw.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rw.Header().Get("Cache-Control"))

	// Verify keep-alive comment was written
	body := rw.Body.String()
	assert.True(t, strings.Contains(body, ": connected"), "expected keep-alive comment, got: %q", body)
}

// TestBracketBrokerMap_BroadcastBracketUpdate verifies that BroadcastBracketUpdate
// sends the correct SSE event format.
func TestBracketBrokerMap_BroadcastBracketUpdate(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	broker := brokers.GetOrCreate(99)
	ch := broker.Subscribe()
	defer broker.Unsubscribe(ch)

	brokers.BroadcastBracketUpdate(99)

	select {
	case msg := <-ch:
		s := string(msg)
		assert.True(t, strings.HasPrefix(s, "event: bracket-update\n"), "unexpected event format: %q", s)
		// Parse the SSE event
		scanner := bufio.NewScanner(strings.NewReader(s))
		lines := map[string]string{}
		for scanner.Scan() {
			line := scanner.Text()
			if idx := strings.Index(line, ": "); idx >= 0 {
				lines[line[:idx]] = line[idx+2:]
			}
		}
		assert.Equal(t, "bracket-update", lines["event"])
		assert.Equal(t, "99", lines["data"])
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for broadcast")
	}
}

// TestBracketBrokerMap_GetOrCreate_SameInstance verifies repeated calls return the same broker.
func TestBracketBrokerMap_GetOrCreate_SameInstance(t *testing.T) {
	brokers := handlers.NewBracketBrokerMap()
	b1 := brokers.GetOrCreate(10)
	b2 := brokers.GetOrCreate(10)
	assert.Same(t, b1, b2, "GetOrCreate should return the same broker for the same tournament ID")

	b3 := brokers.GetOrCreate(11)
	assert.NotSame(t, b1, b3, "different tournament IDs should get different brokers")
}
