package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type OrderStage string

const (
	StageCheckout    OrderStage = "checkout"
	StageFulfillment OrderStage = "fulfillment"
	StageReceipt     OrderStage = "receipt"
	StageCustomer    OrderStage = "customer_update"
)

type DeviceUpdate struct {
	OrderID   string     `json:"order_id"`
	DeviceID  string     `json:"device_id"`
	AccountID string     `json:"account_id"`
	Stage     OrderStage `json:"stage"`
	Status    string     `json:"status"`
	Sequence  int        `json:"sequence"`
}

type Decision struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason"`
}

func Decide(previous *DeviceUpdate, next DeviceUpdate) Decision {
	if next.OrderID == "" || next.DeviceID == "" || next.AccountID == "" || next.Status == "" || next.Sequence < 1 {
		return Decision{Reason: "order_id, device_id, account_id, status, and a positive sequence are required"}
	}
	rank := map[OrderStage]int{StageCheckout: 1, StageFulfillment: 2, StageReceipt: 3, StageCustomer: 4}
	nextRank, known := rank[next.Stage]
	if !known {
		return Decision{Reason: "unknown order stage"}
	}
	if previous == nil {
		if next.Stage != StageCheckout {
			return Decision{Reason: "the first update must be checkout"}
		}
		return Decision{Accepted: true, Reason: "checkout opened"}
	}
	if next.OrderID != previous.OrderID || next.DeviceID != previous.DeviceID || next.AccountID != previous.AccountID {
		return Decision{Reason: "order identity changed"}
	}
	if next.Sequence <= previous.Sequence {
		return Decision{Reason: "sequence must increase"}
	}
	previousRank := rank[previous.Stage]
	if nextRank < previousRank || nextRank > previousRank+1 {
		return Decision{Reason: "stage transition is out of order"}
	}
	return Decision{Accepted: true, Reason: "update accepted"}
}

type Publisher interface {
	Publish(context.Context, DeviceUpdate) error
}

type Store struct {
	mu      sync.RWMutex
	latest  map[string]DeviceUpdate
	clients map[chan DeviceUpdate]struct{}
}

func NewStore() *Store {
	return &Store{latest: make(map[string]DeviceUpdate), clients: make(map[chan DeviceUpdate]struct{})}
}

func (s *Store) Accept(update DeviceUpdate) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, found := s.latest[update.OrderID]
	var prior *DeviceUpdate
	if found {
		prior = &previous
	}
	decision := Decide(prior, update)
	if decision.Accepted {
		s.latest[update.OrderID] = update
	}
	return decision
}

func (s *Store) Broadcast(update DeviceUpdate) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for client := range s.clients {
		select {
		case client <- update:
		default:
		}
	}
}

func (s *Store) Subscribe() (<-chan DeviceUpdate, func()) {
	updates := make(chan DeviceUpdate, 8)
	s.mu.Lock()
	s.clients[updates] = struct{}{}
	s.mu.Unlock()
	return updates, func() {
		s.mu.Lock()
		delete(s.clients, updates)
		close(updates)
		s.mu.Unlock()
	}
}

type Service struct {
	store     *Store
	publisher Publisher
	metrics   http.HandlerFunc
}

func NewService(store *Store, publisher Publisher, metrics http.HandlerFunc) *Service {
	return &Service{store: store, publisher: publisher, metrics: metrics}
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/status", s.receive)
	mux.HandleFunc("GET /dashboard/stream", s.stream)
	mux.HandleFunc("GET /dashboard/metrics", s.metrics)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

func (s *Service) receive(w http.ResponseWriter, r *http.Request) {
	var update DeviceUpdate
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid update payload"})
		return
	}
	decision := s.store.Accept(update)
	if !decision.Accepted {
		writeJSON(w, http.StatusConflict, decision)
		return
	}
	if err := s.publisher.Publish(r.Context(), update); err != nil {
		writeUpstreamError(w, err)
		return
	}
	s.store.Broadcast(update)
	writeJSON(w, http.StatusAccepted, decision)
}

func (s *Service) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	updates, unsubscribe := s.store.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-updates:
			payload, _ := json.Marshal(update)
			fmt.Fprintf(w, "event: order_status\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func writeUpstreamError(w http.ResponseWriter, err error) {
	var apiErr *InfraiError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		writeJSON(w, apiErr.Status, map[string]string{"error": apiErr.Message, "code": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func parseRetryAfter(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		return seconds
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return fallback
}
