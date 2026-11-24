package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/ingest"
)

const (
	maxResponseBytes = 4 << 20
	maxPages         = 20
)

// Subscription is one structured observation from an API connector.
type Subscription struct {
	ExternalID string
	Title      string
	Content    string
	VendorHint string
	OccurredAt time.Time
	Proposal   ingest.Proposal
}

// Client fetches subscriptions from declarative API connectors.
type Client struct {
	HTTP *http.Client
}

// NewClient builds a client with a per-request timeout.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{HTTP: &http.Client{Timeout: timeout}}
}

// Subscriptions fetches up to maxItems subscriptions. The token is sent as a
// bearer credential and is never included in errors.
func (c *Client) Subscriptions(ctx context.Context, connector Connector, token string, maxItems int) ([]Subscription, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("an API token is required")
	}
	if maxItems <= 0 {
		maxItems = 100
	}
	if maxItems > 1000 {
		maxItems = 1000
	}
	base, err := url.Parse(connector.BaseURL)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("invalid API base URL for %s", connector.Name)
	}
	if base.Scheme != "https" && !isLoopbackHost(base.Hostname()) {
		return nil, fmt.Errorf("API connector %s must use https", connector.Name)
	}

	client := c.HTTP
	if client == nil {
		client = NewClient(30 * time.Second).HTTP
	}

	pageURL, err := firstPageURL(connector, base)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	out := make([]Subscription, 0, maxItems)
	for page := 0; page < maxPages && pageURL != nil && len(out) < maxItems; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		document, err := fetchPage(ctx, client, connector, pageURL, token)
		if err != nil {
			return nil, err
		}
		items := sliceAt(document, connector.Mapping.Items)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			subscription, ok := mapSubscription(connector, item, now)
			if !ok {
				continue
			}
			out = append(out, subscription)
			if len(out) >= maxItems {
				break
			}
		}
		pageURL, err = nextPageURL(connector, base, pageURL, document)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func firstPageURL(connector Connector, base *url.URL) (*url.URL, error) {
	page := *base
	page.Path = strings.TrimRight(page.Path, "/") + "/" + strings.TrimLeft(connector.SubscriptionsPath, "/")
	query := page.Query()
	if connector.PageSizeParam != "" && connector.PageSize > 0 {
		query.Set(connector.PageSizeParam, strconv.Itoa(connector.PageSize))
	}
	for _, pair := range connector.ExtraQuery {
		query.Add(pair[0], pair[1])
	}
	page.RawQuery = query.Encode()
	return &page, nil
}

func nextPageURL(connector Connector, base, current *url.URL, document map[string]any) (*url.URL, error) {
	switch connector.Pagination {
	case PaginationStripe:
		if !boolAt(document, "has_more") {
			return nil, nil
		}
		items := sliceAt(document, connector.Mapping.Items)
		if len(items) == 0 {
			return nil, nil
		}
		last, ok := items[len(items)-1].(map[string]any)
		if !ok {
			return nil, nil
		}
		lastID := stringAt(last, connector.Mapping.ID)
		if lastID == "" {
			return nil, nil
		}
		next := *current
		query := next.Query()
		query.Set("starting_after", lastID)
		next.RawQuery = query.Encode()
		return &next, nil
	case PaginationPaddle:
		raw := stringAt(document, "meta.pagination.next")
		if raw == "" {
			return nil, nil
		}
		next, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid next page URL from %s", connector.Name)
		}
		if !next.IsAbs() {
			next = current.ResolveReference(next)
		}
		if next.Scheme != base.Scheme || !strings.EqualFold(next.Host, base.Host) {
			return nil, fmt.Errorf("next page URL from %s left the API host", connector.Name)
		}
		return next, nil
	default:
		return nil, nil
	}
}

func fetchPage(ctx context.Context, client *http.Client, connector Connector, pageURL *url.URL, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Substrackt/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", connector.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", connector.Name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s returned HTTP %d: %s", connector.Name, resp.StatusCode, apiError(body))
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", connector.Name, err)
	}
	return document, nil
}

func apiError(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error.Message != "" {
		return payload.Error.Message
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300]
	}
	if text == "" {
		return "empty error response"
	}
	return text
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func mapSubscription(connector Connector, raw map[string]any, now time.Time) (Subscription, bool) {
	id := stringAt(raw, connector.Mapping.ID)
	if id == "" {
		return Subscription{}, false
	}
	lineItems := sliceAt(raw, connector.Mapping.LineItems)
	if len(lineItems) == 0 {
		return Subscription{}, false
	}
	line, ok := lineItems[0].(map[string]any)
	if !ok {
		return Subscription{}, false
	}

	amount := int64At(line, connector.Mapping.Amount)
	currency := strings.ToUpper(stringAt(line, connector.Mapping.Currency))
	plan := stringAt(line, connector.Mapping.Plan)
	if plan == "" && connector.Mapping.PlanFallback != "" {
		plan = stringAt(line, connector.Mapping.PlanFallback)
	}
	status := connector.Mapping.Statuses[strings.ToLower(stringAt(raw, connector.Mapping.Status))]
	if !status.Valid() {
		status = domain.StatusActive
	}
	unit := connector.Mapping.Intervals[strings.ToLower(stringAt(line, connector.Mapping.Interval))]
	if !unit.Valid() {
		unit = domain.UnitMonth
	}
	count := int(int64At(line, connector.Mapping.IntervalCount))
	if count <= 0 {
		count = 1
	}

	created := timeAt(raw, connector.Mapping.CreatedAt)
	if created.IsZero() {
		created = now
	}
	nextRenewal := timeAt(raw, connector.Mapping.NextRenewal)
	if nextRenewal.IsZero() && connector.Mapping.NextRenewalFallback != "" {
		nextRenewal = timeAt(line, connector.Mapping.NextRenewalFallback)
	}
	customer := stringAt(raw, connector.Mapping.Customer)
	name := plan
	if name == "" {
		name = connector.Vendor
	}

	interval := domain.Interval{Count: count, Unit: unit}
	proposal := ingest.Proposal{
		VendorName:  connector.Vendor,
		Name:        name,
		Plan:        plan,
		AmountMinor: amount,
		Currency:    currency,
		Scale:       connector.Mapping.Scale,
		Interval:    interval,
		AnchorDate:  domain.NewDate(created),
		Status:      status,
		Confidence:  1,
		PaymentHint: connector.Name,
	}
	if !nextRenewal.IsZero() {
		proposal.NextRenewal = domain.NewDate(nextRenewal)
	}

	content := strings.Join([]string{
		fmt.Sprintf("%s subscription %s", connector.Name, id),
		"Status: " + string(status),
		"Plan: " + name,
		fmt.Sprintf("Amount: %d %s (scale %d)", amount, currency, connector.Mapping.Scale),
		fmt.Sprintf("Interval: every %d %s", count, unit),
		"Customer: " + customer,
		"Started: " + proposal.AnchorDate.String(),
	}, "\n")

	title := connector.Vendor + " subscription"
	if plan != "" {
		title += ": " + plan
	}
	return Subscription{
		ExternalID: connector.ID + ":" + id,
		Title:      title,
		Content:    content,
		VendorHint: connector.Vendor,
		OccurredAt: created,
		Proposal:   proposal,
	}, true
}

func lookup(root map[string]any, path string) any {
	if path == "" {
		return nil
	}
	var current any = root
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = object[part]
		if !ok {
			return nil
		}
	}
	return current
}

func stringAt(root map[string]any, path string) string {
	value := lookup(root, path)
	if value == nil && strings.Contains(path, ".") {
		value = lookup(root, strings.SplitN(path, ".", 2)[0])
	}
	return asString(value)
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case map[string]any:
		for _, key := range []string{"name", "display_name", "email", "id"} {
			if text := asString(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func int64At(root map[string]any, path string) int64 {
	return asInt64(lookup(root, path))
}

func asInt64(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		if number, err := typed.Float64(); err == nil {
			return int64(number)
		}
	case float64:
		return int64(typed)
	case string:
		if integer, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return integer
		}
		if number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return int64(number)
		}
	}
	return 0
}

func boolAt(root map[string]any, path string) bool {
	value, ok := lookup(root, path).(bool)
	return ok && value
}

func sliceAt(root map[string]any, path string) []any {
	switch typed := lookup(root, path).(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = typed[i]
		}
		return out
	default:
		return nil
	}
}

func timeAt(root map[string]any, path string) time.Time {
	return asTime(lookup(root, path))
}

func asTime(value any) time.Time {
	switch typed := value.(type) {
	case json.Number:
		if seconds, err := typed.Int64(); err == nil {
			return unixTime(seconds)
		}
		if number, err := typed.Float64(); err == nil {
			return unixTime(int64(number))
		}
	case float64:
		return unixTime(int64(typed))
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed
			}
		}
		if seconds, err := strconv.ParseInt(text, 10, 64); err == nil {
			return unixTime(seconds)
		}
	}
	return time.Time{}
}

func unixTime(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}
