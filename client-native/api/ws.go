package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type WSClient struct {
	url        string
	token      string
	conn       *websocket.Conn
	events     chan WSEvent
	done       chan struct{}
	mu         sync.Mutex
	onReconnect func()
	invalidate  func()
	connected   atomic.Bool
}

// IsConnected returns the WS connection state (thread-safe)
func (ws *WSClient) IsConnected() bool {
	return ws.connected.Load()
}

func NewWSClient(baseURL, token string, invalidate func()) *WSClient {
	// http → ws, https → wss
	wsURL := "ws" + baseURL[4:] + "/api/ws"
	if baseURL[:5] == "https" {
		wsURL = "wss" + baseURL[5:] + "/api/ws"
	}

	return &WSClient{
		url:        wsURL,
		token:      token,
		events:     make(chan WSEvent, 500),
		done:       make(chan struct{}),
		invalidate: invalidate,
	}
}

func (ws *WSClient) dialOpts() *websocket.DialOptions {
	return &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + ws.token},
		},
	}
}

func (ws *WSClient) Connect(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, ws.url, ws.dialOpts())
	if err != nil {
		return fmt.Errorf("WS connect: %w", err)
	}
	conn.SetReadLimit(10 << 20) // 10MB
	ws.mu.Lock()
	ws.conn = conn
	ws.mu.Unlock()
	ws.connected.Store(true)

	go ws.readLoop(ctx)
	return nil
}

func (ws *WSClient) readLoop(ctx context.Context) {
	defer func() {
		select {
		case <-ws.done:
		default:
			close(ws.done)
		}
	}()

	for {
		_, data, err := ws.conn.Read(ctx)
		if err != nil {
			log.Printf("WS read error: %v", err)
			// Reconnect
			go ws.reconnect(ctx)
			return
		}

		var event WSEvent
		if err := json.Unmarshal(data, &event); err != nil {
			log.Printf("WS unmarshal error: %v", err)
			continue
		}

		select {
		case ws.events <- event:
		default:
			// Channel full, drop oldest event
			dropped := <-ws.events
			ws.events <- event
			log.Printf("WS event buffer full, dropped event: %s", dropped.Type)
		}

		if ws.invalidate != nil {
			ws.invalidate()
		}
	}
}

func (ws *WSClient) reconnect(ctx context.Context) {
	ws.connected.Store(false)
	ws.mu.Lock()
	if ws.conn != nil {
		ws.conn.Close(websocket.StatusNormalClosure, "reconnect")
	}
	ws.mu.Unlock()
	if ws.invalidate != nil {
		ws.invalidate()
	}

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}

		conn, _, err := websocket.Dial(ctx, ws.url, ws.dialOpts())
		if err != nil {
			log.Printf("WS reconnect failed: %v, retrying...", err)
			continue
		}
		conn.SetReadLimit(1 << 20)

		ws.mu.Lock()
		ws.conn = conn
		ws.done = make(chan struct{})
		onReconnect := ws.onReconnect
		ws.mu.Unlock()
		ws.connected.Store(true)

		log.Println("WS reconnected")
		if onReconnect != nil {
			onReconnect()
		}

		go ws.readLoop(ctx)
		return
	}
}

func (ws *WSClient) Events() <-chan WSEvent {
	return ws.events
}

func (ws *WSClient) Done() <-chan struct{} {
	return ws.done
}

func (ws *WSClient) Close() {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.conn != nil {
		ws.conn.Close(websocket.StatusNormalClosure, "bye")
	}
}

// SendJSON sends a typed event with an arbitrary payload (marshalled to JSON).
func (ws *WSClient) SendJSON(eventType string, payload any) error {
	p, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return ws.Send(WSEvent{Type: eventType, Payload: p})
}

func (ws *WSClient) Send(event WSEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.conn == nil {
		return fmt.Errorf("WS not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return ws.conn.Write(ctx, websocket.MessageText, data)
}

func (ws *WSClient) SetOnReconnect(fn func()) {
	ws.mu.Lock()
	ws.onReconnect = fn
	ws.mu.Unlock()
}

func (ws *WSClient) UpdateToken(token string) {
	ws.mu.Lock()
	ws.token = token
	ws.mu.Unlock()
}
