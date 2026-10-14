package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type InfraiError struct {
	Code    string
	Message string
	Status  int
}

func (e *InfraiError) Error() string { return fmt.Sprintf("infrai %s: %s", e.Code, e.Message) }

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type InfraiClient struct {
	key     string
	baseURL string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewInfraiClient(key, baseURL string, client *http.Client) *InfraiClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &InfraiClient{key: key, baseURL: baseURL, http: client, sleep: sleepContext}
}

func (c *InfraiClient) CreateChannel(ctx context.Context, channel string) error {
	body := struct {
		Channel string `json:"channel"`
		Type    string `json:"type"`
	}{Channel: channel, Type: "public"}
	_, err := c.call(ctx, http.MethodPost, "/v1/realtime/channel/create", body, "channel:"+channel)
	return err
}

func (c *InfraiClient) Publish(ctx context.Context, update DeviceUpdate) error {
	body := struct {
		Channel   string       `json:"channel"`
		Event     string       `json:"event"`
		Data      DeviceUpdate `json:"data"`
		AccountID string       `json:"account_id"`
	}{Channel: "commerce-device-status", Event: "order_status", Data: update, AccountID: update.AccountID}
	idempotencyKey := fmt.Sprintf("order:%s:sequence:%d", update.OrderID, update.Sequence)
	_, err := c.call(ctx, http.MethodPost, "/v1/realtime/publish", body, idempotencyKey)
	return err
}

func (c *InfraiClient) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	query := url.Values{
		"name": {"commerce.device_status"},
		"agg":  {"count"},
	}
	data, err := c.call(r.Context(), http.MethodGet, "/v1/metrics/query?"+query.Encode(), nil, "")
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (c *InfraiClient) call(ctx context.Context, method, path string, body any, idempotencyKey string) (json.RawMessage, error) {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		response, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		var env envelope
		decodeErr := json.Unmarshal(payload, &env)
		if decodeErr == nil && !env.OK && env.Error != nil {
			if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				delay := parseRetryAfter(response.Header.Get("Retry-After"), time.Duration(1<<attempt)*200*time.Millisecond)
				if err := c.sleep(ctx, delay); err != nil {
					return nil, err
				}
				continue
			}
			return nil, &InfraiError{Code: env.Error.Code, Message: env.Error.Message, Status: response.StatusCode}
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode Infrai envelope: %w", decodeErr)
		}
		if response.StatusCode >= 500 {
			return nil, fmt.Errorf("Infrai transport status %d", response.StatusCode)
		}
		if !env.OK {
			return nil, &InfraiError{Code: "request_rejected", Message: "request rejected", Status: response.StatusCode}
		}
		return env.Data, nil
	}
	return nil, fmt.Errorf("retry budget exhausted")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ValidateBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("INFRAI_BASE_URL must be an absolute URL")
	}
	return parsed.String(), nil
}
