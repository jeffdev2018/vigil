// Package push delivers mobile push notifications (K64) through the Expo
// push service: the app registers its Expo token, the server posts
// messages to exp.host. No Apple / Google credentials live here.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const DefaultExpoEndpoint = "https://exp.host/--/api/v2/push/send"

// Message is one notification to one device token.
type Message struct {
	To    string         `json:"to"`
	Title string         `json:"title,omitempty"`
	Body  string         `json:"body,omitempty"`
	Badge *int           `json:"badge,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
	Sound string         `json:"sound,omitempty"`
}

// Sender delivers a batch; each ticket reports one message's acceptance.
type Sender interface {
	Send(ctx context.Context, msgs []Message) ([]Ticket, error)
}

type Ticket struct {
	Status  string `json:"status"`
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
	Details struct {
		Error string `json:"error,omitempty"`
	} `json:"details,omitempty"`
}

// ExpoSender posts to the Expo push API. Batches of at most 100 messages.
type ExpoSender struct {
	Endpoint string
	Client   *http.Client
}

func NewExpoSender(endpoint string) *ExpoSender {
	if endpoint == "" {
		endpoint = DefaultExpoEndpoint
	}
	return &ExpoSender{Endpoint: endpoint, Client: &http.Client{Timeout: 15 * time.Second}}
}

// IsExpoToken reports whether the token looks like an Expo push token.
func IsExpoToken(token string) bool {
	return strings.HasPrefix(token, "ExponentPushToken[") || strings.HasPrefix(token, "ExpoPushToken[")
}

func (e *ExpoSender) Send(ctx context.Context, msgs []Message) ([]Ticket, error) {
	var tickets []Ticket
	for start := 0; start < len(msgs); start += 100 {
		end := start + 100
		if end > len(msgs) {
			end = len(msgs)
		}
		body, err := json.Marshal(msgs[start:end])
		if err != nil {
			return tickets, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, bytes.NewReader(body))
		if err != nil {
			return tickets, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := e.Client.Do(req)
		if err != nil {
			return tickets, err
		}
		var out struct {
			Data []Ticket `json:"data"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return tickets, fmt.Errorf("expo push: status %d", resp.StatusCode)
		}
		if decodeErr != nil {
			return tickets, errors.New("expo push: malformed response")
		}
		tickets = append(tickets, out.Data...)
	}
	return tickets, nil
}
