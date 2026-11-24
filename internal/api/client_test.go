package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"substrackt/internal/domain"
)

func TestStripeConnectorMapsAndPaginates(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if r.URL.Path != "/v1/subscriptions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("status") != "all" || query.Get("limit") != "100" {
			t.Fatalf("query = %v", query)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = w.Write([]byte(`{
				"has_more": true,
				"data": [
					{"id":"sub_1","status":"active","start_date":1700000000,"current_period_end":1800000000,
					 "customer":{"name":"Acme"},
					 "items":{"data":[{"price":{"id":"price_1","unit_amount":1200,"currency":"usd","nickname":"Pro","recurring":{"interval":"month","interval_count":1}}}]}}
				]
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"has_more": false,
			"data": [
				{"id":"sub_2","status":"trialing","start_date":1700000000,
				 "customer":{"email":"me@example.com"},
				 "items":{"data":[{"price":{"id":"price_2","unit_amount":9900,"currency":"eur","recurring":{"interval":"year","interval_count":1}}}]}}
			]
		}`))
	}))
	defer server.Close()

	connector, err := ConnectorFor("stripe")
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	connector.BaseURL = server.URL + "/v1"
	client := NewClient(5 * time.Second)
	items, err := client.Subscriptions(context.Background(), connector, "test-token", 10)
	if err != nil {
		t.Fatalf("subscriptions: %v", err)
	}
	if requests != 2 || len(items) != 2 {
		t.Fatalf("requests = %d, items = %+v", requests, items)
	}
	first := items[0]
	if first.ExternalID != "stripe:sub_1" || first.Proposal.AmountMinor != 1200 {
		t.Fatalf("first = %+v", first)
	}
	if first.Proposal.Currency != "USD" || first.Proposal.Interval != (domain.Interval{Count: 1, Unit: domain.UnitMonth}) {
		t.Fatalf("first proposal = %+v", first.Proposal)
	}
	if first.Proposal.AnchorDate.String() != "2023-11-14" {
		t.Fatalf("anchor = %s", first.Proposal.AnchorDate)
	}
	if first.Proposal.Status != domain.StatusActive || first.Proposal.Confidence != 1 {
		t.Fatalf("first status = %+v", first.Proposal)
	}
	second := items[1]
	if second.Proposal.Status != domain.StatusTrial || second.Proposal.Plan != "price_2" {
		t.Fatalf("second = %+v", second.Proposal)
	}
	if second.Proposal.Currency != "EUR" || second.Proposal.Interval.Unit != domain.UnitYear {
		t.Fatalf("second proposal = %+v", second.Proposal)
	}
}

func TestPaddleConnectorFollowsNextLink(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer paddle-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "" {
			_, _ = w.Write([]byte(`{
				"data": [
					{"id":"sub_1","status":"active","created_at":"2024-01-02T03:04:05Z","next_billed_at":"2024-02-02T03:04:05Z","customer_id":"ctm_1",
					 "items":[{"price":{"id":"pri_1","name":"Starter","unit_price":{"amount":"500","currency_code":"USD"},"billing_cycle":{"interval":"month","frequency":1}}}]}
				],
				"meta":{"pagination":{"next":"` + baseURL + `/subscriptions?after=sub_1"}}
			}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"data": [
				{"id":"sub_2","status":"canceled","created_at":"2024-01-02T03:04:05Z","customer_id":"ctm_2",
				 "items":[{"price":{"id":"pri_2","name":"Team","unit_price":{"amount":"12000","currency_code":"USD"},"billing_cycle":{"interval":"year","frequency":1}}}]}
			],
			"meta":{"pagination":{"next":null}}
		}`))
	}))
	defer server.Close()
	baseURL = server.URL

	connector, err := ConnectorFor("paddle")
	if err != nil {
		t.Fatalf("connector: %v", err)
	}
	connector.BaseURL = server.URL
	items, err := NewClient(5*time.Second).Subscriptions(context.Background(), connector, "paddle-token", 10)
	if err != nil {
		t.Fatalf("subscriptions: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Proposal.Plan != "Starter" || items[0].Proposal.AmountMinor != 500 {
		t.Fatalf("first = %+v", items[0].Proposal)
	}
	if items[1].Proposal.Status != domain.StatusCancelled || items[1].Proposal.Interval.Unit != domain.UnitYear {
		t.Fatalf("second = %+v", items[1].Proposal)
	}
}

func TestSubscriptionsRejectsMissingToken(t *testing.T) {
	connector, _ := ConnectorFor("stripe")
	if _, err := NewClient(time.Second).Subscriptions(context.Background(), connector, "", 10); err == nil {
		t.Fatal("expected a missing token to be rejected")
	}
}

func TestAPIErrorDoesNotIncludeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	connector, _ := ConnectorFor("stripe")
	connector.BaseURL = server.URL
	_, err := NewClient(time.Second).Subscriptions(context.Background(), connector, "secret-token", 10)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error = %v", err)
	}
}
