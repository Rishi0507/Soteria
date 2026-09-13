package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---- Slack incoming webhook -------------------------------------------------

// Slack posts to an incoming-webhook URL (free on any workspace).
type Slack struct {
	WebhookURL string
	HTTP       *http.Client
}

// NewSlack returns a Slack channel.
func NewSlack(webhookURL string) *Slack {
	return &Slack{WebhookURL: webhookURL, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (s *Slack) Name() string { return ChannelSlack }

func (s *Slack) Send(ctx context.Context, m Message) (string, error) {
	if s.WebhookURL == "" {
		return "", Permanent(errors.New("slack: webhook URL not configured"))
	}
	body, _ := json.Marshal(map[string]any{
		"text": m.Subject + "\n" + m.Text,
		"blocks": []map[string]any{
			{"type": "header", "text": map[string]any{"type": "plain_text", "text": truncate(m.Subject, 150), "emoji": true}},
			{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": truncate(m.Text, 2900)}},
		},
	})
	resp, err := post(ctx, s.HTTP, s.WebhookURL, "application/json", body, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err := classify(resp.StatusCode, "slack", data); err != nil {
		return "", err
	}
	return "slack:" + time.Now().UTC().Format(time.RFC3339Nano), nil
}

// ---- Resend transactional email ------------------------------------------

// Resend sends email through https://resend.com (free tier: 3,000/month).
// SendGrid no longer offers a permanent free tier, which is why Resend.
type Resend struct {
	APIKey string
	From   string // "Sotería Recall Alerts <alerts@yourdomain.com>"; onboarding@resend.dev works before a domain is verified
	HTTP   *http.Client
	URL    string
}

// NewResend returns an email channel.
func NewResend(apiKey, from string) *Resend {
	return &Resend{APIKey: apiKey, From: from, HTTP: &http.Client{Timeout: 20 * time.Second}, URL: "https://api.resend.com/emails"}
}

func (r *Resend) Name() string { return ChannelEmail }

func (r *Resend) Send(ctx context.Context, m Message) (string, error) {
	if r.APIKey == "" {
		return "", Permanent(errors.New("resend: API key not configured"))
	}
	if !strings.Contains(m.Recipient, "@") {
		return "", Permanent(fmt.Errorf("resend: invalid recipient %q", Mask(m.Recipient)))
	}
	payload := map[string]any{"from": r.From, "to": []string{m.Recipient}, "subject": m.Subject, "text": m.Text}
	if m.HTML != "" {
		payload["html"] = m.HTML
	}
	body, _ := json.Marshal(payload)
	resp, err := post(ctx, r.HTTP, r.URL, "application/json", body, map[string]string{"Authorization": "Bearer " + r.APIKey})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err := classify(resp.StatusCode, "resend", data); err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &out)
	if out.ID == "" {
		out.ID = "resend:unknown"
	}
	return out.ID, nil
}

// ---- shared ------------------------------------------------------------------

func post(ctx context.Context, c *http.Client, url, contentType string, body []byte, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, Permanent(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Soteria-Notification/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err // network: transient
	}
	return resp, nil
}

// classify maps provider status codes: 2xx ok; 429/5xx transient; other 4xx permanent.
func classify(status int, provider string, body []byte) error {
	switch {
	case status/100 == 2:
		return nil
	case status == http.StatusTooManyRequests || status >= 500:
		return fmt.Errorf("%s: HTTP %d: %s", provider, status, truncate(string(body), 200))
	default:
		return Permanent(fmt.Errorf("%s: HTTP %d: %s", provider, status, truncate(string(body), 200)))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
