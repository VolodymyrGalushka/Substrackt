// Package llm talks to a local OpenAI-compatible endpoint. It deliberately
// refuses public hosts: subscription data never leaves the machine or the
// user's own network.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PromptVersion is part of the extraction cache key. Bump it whenever the
// prompt or schema changes so stale results are not reused.
const PromptVersion = "extract.v1"

// Client is a minimal OpenAI-compatible chat client.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New validates the endpoint and returns a client. baseURL must be loopback, a
// private address, or a local service name.
func New(baseURL, model string, timeout time.Duration) (*Client, error) {
	normalized, err := ValidateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("LLM model is required")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		baseURL: normalized,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// BaseURL returns the normalised endpoint.
func (c *Client) BaseURL() string { return c.baseURL }

// Model returns the configured model name.
func (c *Client) Model() string { return c.model }

// ValidateBaseURL normalises an endpoint and rejects anything that is not on the
// local machine or the user's private network.
func ValidateBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("LLM endpoint is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid LLM endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("LLM endpoint must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("LLM endpoint has no host")
	}
	if u.User != nil {
		return "", fmt.Errorf("LLM endpoint must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("LLM endpoint must not contain a query or fragment")
	}
	if !hostIsLocal(u.Hostname()) {
		return "", fmt.Errorf("LLM endpoint %q is not local; only local or self-hosted endpoints are allowed", u.Hostname())
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/v1"
	}
	u.Path = path
	return u.String(), nil
}

func hostIsLocal(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	switch host {
	case "localhost", "host.docker.internal", "gateway.docker.internal":
		return true
	}
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	for _, suffix := range []string{".local", ".internal", ".home.arpa", ".lan", ".localdomain"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	// A bare service name (docker compose, a home-lab DNS name) has no dot.
	return !strings.Contains(host, ".")
}

// Extraction is the schema the model must return.
type Extraction struct {
	IsSubscription bool    `json:"is_subscription"`
	Vendor         string  `json:"vendor"`
	Name           string  `json:"name"`
	Plan           string  `json:"plan"`
	Amount         string  `json:"amount"`
	Currency       string  `json:"currency"`
	IntervalCount  int     `json:"interval_count"`
	IntervalUnit   string  `json:"interval_unit"`
	NextRenewal    string  `json:"next_renewal"`
	Status         string  `json:"status"`
	PaymentHint    string  `json:"payment_hint"`
	Confidence     float64 `json:"confidence"`
	Evidence       string  `json:"evidence"`
	Reasoning      string  `json:"reasoning"`
}

// Meta describes the model calls behind an extraction.
type Meta struct {
	Model            string
	PromptVersion    string
	PromptTokens     int
	CompletionTokens int
	Repairs          int
	Latency          time.Duration
	Cached           bool
}

// Normalize cleans model output and applies safe defaults.
func (e *Extraction) Normalize() {
	e.Vendor = strings.TrimSpace(e.Vendor)
	e.Name = strings.TrimSpace(e.Name)
	e.Plan = strings.TrimSpace(e.Plan)
	e.Amount = strings.TrimSpace(e.Amount)
	e.Currency = strings.ToUpper(strings.TrimSpace(e.Currency))
	e.IntervalUnit = strings.ToLower(strings.TrimSpace(e.IntervalUnit))
	e.NextRenewal = strings.TrimSpace(e.NextRenewal)
	e.Status = strings.ToLower(strings.TrimSpace(e.Status))
	e.PaymentHint = strings.TrimSpace(e.PaymentHint)
	e.Evidence = strings.TrimSpace(e.Evidence)
	e.Reasoning = strings.TrimSpace(e.Reasoning)

	if e.IsSubscription {
		if e.Name == "" {
			e.Name = e.Vendor
		}
		if e.IntervalCount <= 0 {
			e.IntervalCount = 1
		}
		if e.IntervalUnit == "" {
			e.IntervalUnit = "month"
		}
		if e.Status == "" {
			e.Status = "active"
		}
		if e.Confidence > 1 && e.Confidence <= 100 {
			e.Confidence /= 100
		}
	}
	if len(e.Evidence) > 1000 {
		e.Evidence = e.Evidence[:1000]
	}
	if len(e.Reasoning) > 1000 {
		e.Reasoning = e.Reasoning[:1000]
	}
}

// Validate checks the model's answer before it is trusted.
func (e Extraction) Validate() error {
	if !e.IsSubscription {
		return nil
	}
	if e.Vendor == "" {
		return fmt.Errorf("vendor is required")
	}
	if e.IntervalCount <= 0 {
		return fmt.Errorf("interval_count must be positive")
	}
	switch e.IntervalUnit {
	case "day", "week", "month", "year":
	default:
		return fmt.Errorf("invalid interval_unit %q", e.IntervalUnit)
	}
	if e.Amount != "" {
		if !validAmount(e.Amount) {
			return fmt.Errorf("amount %q is not a decimal number", e.Amount)
		}
	}
	if e.Currency != "" && !validCurrency(e.Currency) {
		return fmt.Errorf("invalid currency %q", e.Currency)
	}
	if e.NextRenewal != "" {
		if _, err := time.Parse("2006-01-02", e.NextRenewal); err != nil {
			return fmt.Errorf("next_renewal %q is not YYYY-MM-DD", e.NextRenewal)
		}
	}
	switch e.Status {
	case "active", "trial", "cancelled":
	default:
		return fmt.Errorf("invalid status %q", e.Status)
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %v out of range 0-1", e.Confidence)
	}
	return nil
}

func validAmount(s string) bool {
	digits := 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.' || r == ',' || r == ' ':
		case (r == '-' || r == '+') && i == 0:
		default:
			return false
		}
	}
	return digits > 0
}

func validCurrency(s string) bool {
	if len(s) < 2 || len(s) > 10 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

const systemPrompt = `You extract subscription billing details from receipts, invoices and email.
Return exactly one JSON object and nothing else. Use this schema:
{"is_subscription":true,"vendor":"","name":"","plan":"","amount":"","currency":"","interval_count":1,"interval_unit":"month","next_renewal":"","status":"active","payment_hint":"","confidence":0.0,"evidence":"","reasoning":""}
Rules:
- is_subscription is false for one-off purchases, shipping notices, password resets and marketing email.
- vendor is the service being paid for, not the payment processor.
- amount is the billed total as a decimal string without a currency symbol.
- currency is an ISO 4217 code such as USD or EUR. Use the billing currency, not the card's currency.
- interval_unit is one of day, week, month, year. interval_count is how many units per charge.
- next_renewal is YYYY-MM-DD when the document states the next charge; otherwise "".
- status is active, trial or cancelled.
- payment_hint describes a payment method only as non-sensitive text (for example "Visa ending 4242"). Never include a full card number or security code.
- confidence is 0-1 and must reflect how clearly the document supports the fields.
- evidence is a short verbatim quote from the document. Do not invent values.`

// Models lists the models exposed by the endpoint.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach LLM endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read LLM models: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &statusError{status: resp.StatusCode, body: string(body)}
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode LLM models: %w", err)
	}
	out := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if strings.TrimSpace(m.ID) != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

// Extract asks the model for a schema-validated extraction, retrying once with
// a repair prompt when the answer is unusable.
func (c *Client) Extract(ctx context.Context, content string) (Extraction, Meta, error) {
	start := time.Now()
	meta := Meta{Model: c.model, PromptVersion: PromptVersion}
	content = strings.TrimSpace(content)
	if content == "" {
		return Extraction{}, meta, fmt.Errorf("empty content")
	}
	const maxContent = 12000
	if len(content) > maxContent {
		content = content[:maxContent]
	}

	messages := []message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: "Extract subscription details from this document:\n\n" + content},
	}

	raw, usage, err := c.chat(ctx, messages, true)
	meta.PromptTokens += usage.PromptTokens
	meta.CompletionTokens += usage.CompletionTokens
	if err != nil {
		if isFormatUnsupported(err) {
			raw, usage, err = c.chat(ctx, messages, false)
			meta.PromptTokens += usage.PromptTokens
			meta.CompletionTokens += usage.CompletionTokens
		}
		if err != nil {
			meta.Latency = time.Since(start)
			return Extraction{}, meta, err
		}
	}

	parsed, parseErr := parseExtraction(raw)
	if parseErr == nil {
		meta.Latency = time.Since(start)
		return parsed, meta, nil
	}

	// One repair attempt: show the model its invalid answer and the reason.
	meta.Repairs++
	messages = append(messages,
		message{Role: "assistant", Content: raw},
		message{Role: "user", Content: "That reply was not valid: " + parseErr.Error() +
			". Return only the corrected JSON object, with no prose or code fences."},
	)
	repaired, usage, err := c.chat(ctx, messages, false)
	meta.PromptTokens += usage.PromptTokens
	meta.CompletionTokens += usage.CompletionTokens
	if err != nil {
		meta.Latency = time.Since(start)
		return Extraction{}, meta, err
	}
	parsed, err = parseExtraction(repaired)
	meta.Latency = time.Since(start)
	if err != nil {
		return Extraction{}, meta, fmt.Errorf("model returned invalid extraction after repair: %w", err)
	}
	return parsed, meta, nil
}

func parseExtraction(raw string) (Extraction, error) {
	var parsed Extraction
	cleaned := cleanJSON(raw)
	if cleaned == "" {
		return Extraction{}, fmt.Errorf("empty response")
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return Extraction{}, fmt.Errorf("invalid JSON: %w", err)
	}
	parsed.Normalize()
	if err := parsed.Validate(); err != nil {
		return Extraction{}, err
	}
	return parsed, nil
}

func cleanJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
			raw = raw[idx+1:]
		}
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type usage struct {
	PromptTokens     int
	CompletionTokens int
}

func (c *Client) chat(ctx context.Context, messages []message, jsonMode bool) (string, usage, error) {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: 0,
		Stream:      false,
	}
	if jsonMode {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	encoded, err := json.Marshal(reqBody)
	if err != nil {
		return "", usage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return "", usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage{}, fmt.Errorf("reach LLM endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", usage{}, fmt.Errorf("read LLM response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", usage{}, &statusError{status: resp.StatusCode, body: string(body)}
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", usage{}, fmt.Errorf("decode LLM response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", usage{}, fmt.Errorf("LLM returned no choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", usage{}, fmt.Errorf("LLM returned an empty answer")
	}
	return content, usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}, nil
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	body := strings.TrimSpace(e.body)
	if len(body) > 300 {
		body = body[:300]
	}
	if body == "" {
		return fmt.Sprintf("LLM endpoint returned HTTP %d", e.status)
	}
	return fmt.Sprintf("LLM endpoint returned HTTP %d: %s", e.status, body)
}

func isFormatUnsupported(err error) bool {
	var se *statusError
	if !errorsAs(err, &se) {
		return false
	}
	if se.status != http.StatusBadRequest && se.status != http.StatusUnprocessableEntity {
		return false
	}
	body := strings.ToLower(se.body)
	return strings.Contains(body, "response_format") || strings.Contains(body, "json_object")
}

// errorsAs is a tiny wrapper so this file does not need to import errors just
// for one assertion.
func errorsAs(err error, target **statusError) bool {
	for err != nil {
		if se, ok := err.(*statusError); ok {
			*target = se
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
