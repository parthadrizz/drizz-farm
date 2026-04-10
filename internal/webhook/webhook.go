// Package webhook sends HTTP POST notifications on session events.
package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Event represents a webhook payload.
type Event struct {
	Type      string `json:"type"`       // "session.created", "session.released", "session.timed_out", "pool.exhausted"
	SessionID string `json:"session_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Serial    string `json:"serial,omitempty"`
	Duration  string `json:"duration,omitempty"`
	Timestamp string `json:"timestamp"`
	NodeName  string `json:"node_name,omitempty"`
}

// Sender sends webhook events to configured URLs.
type Sender struct {
	urls   []string
	client *http.Client
}

// NewSender creates a webhook sender for the given URLs.
func NewSender(urls []string) *Sender {
	return &Sender{
		urls: urls,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send fires a webhook event to all configured URLs.
func (s *Sender) Send(event Event) {
	if len(s.urls) == 0 {
		return
	}

	event.Timestamp = time.Now().Format(time.RFC3339)
	body, err := json.Marshal(event)
	if err != nil {
		return
	}

	for _, url := range s.urls {
		go func(u string) {
			resp, err := s.client.Post(u, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Debug().Err(err).Str("url", u).Str("type", event.Type).Msg("webhook: failed")
				return
			}
			resp.Body.Close()
			log.Debug().Str("url", u).Int("status", resp.StatusCode).Str("type", event.Type).Msg("webhook: sent")
		}(url)
	}
}
