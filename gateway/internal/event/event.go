package event

import (
	"fmt"
	"sync"
	"time"
)

// EventType defines the category of an event.
type EventType int

const (
	TypeTick EventType = iota
	TypeOrderBook
	TypeBar
	TypeTrade
	TypeOrderUpdate
	TypeBalance
	TypePosition
	TypeFactor
	TypeSignal
	TypeRiskAlert
	TypeSystem
)

var typeNames = map[EventType]string{
	TypeTick:        "TICK",
	TypeOrderBook:   "ORDERBOOK",
	TypeBar:         "BAR",
	TypeTrade:       "TRADE",
	TypeOrderUpdate: "ORDER_UPDATE",
	TypeBalance:     "BALANCE",
	TypePosition:    "POSITION",
	TypeFactor:      "FACTOR",
	TypeSignal:      "SIGNAL",
	TypeRiskAlert:   "RISK_ALERT",
	TypeSystem:      "SYSTEM",
}

func (e EventType) String() string {
	if name, ok := typeNames[e]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(%d)", e)
}

// Priority for event processing. Lower number = higher priority.
type Priority int

const (
	PrioHigh   Priority = 0
	PrioNormal Priority = 1
	PrioLow    Priority = 2
)

// Event is the universal data envelope passed through the bus.
type Event struct {
	Type      EventType
	Symbol    string
	Data      any
	Timestamp int64
	Priority  Priority
}

// SubscriptionID uniquely identifies a subscription.
type SubscriptionID uint64

// Handler is a callback function for events.
type Handler func(event Event)

type subscription struct {
	id       SubscriptionID
	symbol   string
	evtTypes map[EventType]bool // empty map means all types
	handler  Handler
	priority Priority
}

// EventBus is an asynchronous, priority-aware event bus.
type EventBus struct {
	mu            sync.RWMutex
	subs          map[SubscriptionID]*subscription
	symbolSubs    map[string]map[SubscriptionID]struct{} // symbol -> sub IDs
	nextID        SubscriptionID
	ch            chan Event
	closed        bool
	wg            sync.WaitGroup
	workerCount   int
	backpressureC chan struct{}
}

// NewEventBus creates an event bus with the given buffer size and worker count.
func NewEventBus(bufferSize int, workerCount int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = 10000
	}
	if workerCount <= 0 {
		workerCount = 4
	}
	bus := &EventBus{
		subs:          make(map[SubscriptionID]*subscription),
		symbolSubs:    make(map[string]map[SubscriptionID]struct{}),
		ch:            make(chan Event, bufferSize),
		workerCount:   workerCount,
		backpressureC: make(chan struct{}, 1),
	}
	for i := 0; i < workerCount; i++ {
		bus.wg.Add(1)
		go bus.worker()
	}
	return bus
}

func (b *EventBus) worker() {
	defer b.wg.Done()
	for evt := range b.ch {
		b.dispatch(evt)
	}
}

func (b *EventBus) dispatch(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	// Find matching subscribers
	candidates := make(map[SubscriptionID]*subscription)

	// Add subscribers for this specific symbol
	if symMap, ok := b.symbolSubs[evt.Symbol]; ok {
		for id := range symMap {
			if sub, ok2 := b.subs[id]; ok2 {
				candidates[id] = sub
			}
		}
	}

	// Add global (empty symbol) subscribers
	if symMap, ok := b.symbolSubs[""]; ok {
		for id := range symMap {
			if sub, ok2 := b.subs[id]; ok2 {
				candidates[id] = sub
			}
		}
	}

	// Sort by priority
	type prioSub struct {
		sub *subscription
		prio Priority
	}
	var sorted []prioSub
	for _, sub := range candidates {
		// Check event type match (empty map = all types)
		if len(sub.evtTypes) == 0 {
			sorted = append(sorted, prioSub{sub, sub.priority})
		} else if sub.evtTypes[evt.Type] {
			sorted = append(sorted, prioSub{sub, sub.priority})
		}
	}

	// Stable order: higher priority (lower number) first
	for p := Priority(0); p <= PrioLow; p++ {
		for _, ps := range sorted {
			if ps.prio == p {
				ps.sub.handler(evt)
			}
		}
	}
}

// Subscribe registers a handler for a specific symbol and event types.
// If symbol is "", subscribes to all symbols.
// If eventTypes is empty, subscribes to all event types.
func (b *EventBus) Subscribe(symbol string, priority Priority, handler Handler, eventTypes ...EventType) SubscriptionID {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++

	evtSet := make(map[EventType]bool)
	for _, et := range eventTypes {
		evtSet[et] = true
	}

	sub := &subscription{
		id:       id,
		symbol:   symbol,
		evtTypes: evtSet,
		handler:  handler,
		priority: priority,
	}
	b.subs[id] = sub

	if b.symbolSubs[symbol] == nil {
		b.symbolSubs[symbol] = make(map[SubscriptionID]struct{})
	}
	b.symbolSubs[symbol][id] = struct{}{}

	return id
}

// Unsubscribe removes a subscription by ID.
func (b *EventBus) Unsubscribe(id SubscriptionID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub, ok := b.subs[id]
	if !ok {
		return
	}
	delete(b.symbolSubs[sub.symbol], id)
	delete(b.subs, id)
}

// Publish pushes an event onto the bus. Non-blocking with backpressure.
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	b.mu.RUnlock()

	event.Timestamp = time.Now().UnixNano()

	select {
	case b.ch <- event:
	default:
		// Backpressure: channel full, drop lowest priority or block briefly
		select {
		case b.backpressureC <- struct{}{}:
			b.ch <- event
			<-b.backpressureC
		default:
			// Drop the event if backpressure is already active
		}
	}
}

// PublishBlocking publishes an event and blocks until it's queued.
func (b *EventBus) PublishBlocking(event Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	b.mu.RUnlock()

	event.Timestamp = time.Now().UnixNano()
	b.ch <- event
}

// Close stops all workers and closes the bus.
func (b *EventBus) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	close(b.ch)
	b.wg.Wait()
}

// SubscriberCount returns the number of active subscriptions.
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
