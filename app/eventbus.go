package app

import (
	"sync"
	"sync/atomic"
	"time"
)

// BusMessageType represents the type of event bus message.
type BusMessageType string

const (
	BusMessageCreated         BusMessageType = "created"
	BusMessageStatusChanged   BusMessageType = "status_changed"
	BusMessageDeliveryAttempt BusMessageType = "delivery_attempt"
)

// BusMessage is a message published to the EventBus.
type BusMessage struct {
	ID             uint64            `json:"id"`
	Type           BusMessageType    `json:"type"`
	EventID        string            `json:"event_id"`
	Subject        string            `json:"subject"`
	DeliveryStatus string            `json:"delivery_status"`
	Timestamp      time.Time         `json:"timestamp"`
	Properties     map[string]string `json:"properties,omitempty"`

	// DeliveryAttempt fields (only set for delivery_attempt messages)
	SubscriberEndpoint string `json:"subscriber_endpoint,omitempty"`
	AttemptStatus      string `json:"attempt_status,omitempty"`
	ResponseStatusCode int    `json:"response_status_code,omitempty"`
}

const subscriberBufferSize = 64

// EventBus is an in-memory pub/sub bus for broadcasting event updates to SSE clients.
type EventBus struct {
	nextID      atomic.Uint64
	mu          sync.RWMutex
	subscribers map[chan BusMessage]struct{}
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[chan BusMessage]struct{}),
	}
}

// Subscribe returns a buffered channel that receives bus messages and an
// unsubscribe function. The caller must call unsubscribe when done.
func (b *EventBus) Subscribe() (<-chan BusMessage, func()) {
	ch := make(chan BusMessage, subscriberBufferSize)

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		b.mu.Unlock()
	}

	return ch, unsubscribe
}

// Publish sends a message to all subscribers with a non-blocking send.
// Slow consumers that have full buffers will miss messages.
func (b *EventBus) Publish(msg BusMessage) {
	msg.ID = b.nextID.Add(1)
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- msg:
		default:
			// Drop message for slow consumer
		}
	}
}
