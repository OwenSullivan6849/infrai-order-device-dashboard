package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateChannelOmitsVendor(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, exists := body["vendor"]; exists {
			t.Fatalf("unexpected vendor in channel request: %v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":{},"error":null,"metadata":{}}`))}, nil
	})
	client := NewInfraiClient("test-key", "https://example.test", &http.Client{Transport: transport})
	if err := client.CreateChannel(context.Background(), "commerce-device-status"); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsHandlerIncludesRequiredQuery(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("name"); got != "commerce.device_status" {
			t.Fatalf("name = %q", got)
		}
		if got := r.URL.Query().Get("agg"); got != "count" {
			t.Fatalf("agg = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"data":[],"error":null,"metadata":{}}`))}, nil
	})
	client := NewInfraiClient("test-key", "https://example.test", &http.Client{Transport: transport})
	response := httptest.NewRecorder()
	client.MetricsHandler(response, httptest.NewRequest(http.MethodGet, "/dashboard/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestPublishRequestAndRateLimitRetry(t *testing.T) {
	requests := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/realtime/publish" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Idempotency-Key") != "order:ord-42:sequence:2" {
			t.Fatalf("required request headers were not set")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"channel":"commerce-device-status"`) || !strings.Contains(string(body), `"account_id":"shop-9"`) {
			t.Fatalf("unexpected publish body: %s", body)
		}
		status := http.StatusOK
		payload := `{"ok":true,"data":{"published":true},"error":null,"metadata":{}}`
		if requests == 1 {
			status = http.StatusTooManyRequests
			payload = `{"ok":false,"data":null,"error":{"code":"rate_limited","message":"retry later"},"metadata":{}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	client := NewInfraiClient("test-key", "https://example.test", &http.Client{Transport: transport})
	client.sleep = func(context.Context, time.Duration) error { return nil }
	update := DeviceUpdate{OrderID: "ord-42", DeviceID: "pos-7", AccountID: "shop-9", Stage: StageFulfillment, Status: "packed", Sequence: 2}
	if err := client.Publish(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}
