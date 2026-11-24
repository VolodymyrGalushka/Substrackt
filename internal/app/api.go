package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/api"
	"substrackt/internal/domain"
	"substrackt/internal/ingest"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

// APIJobKind is the queue kind for one vendor API sync.
const APIJobKind = "api.sync"

type apiPayload struct {
	SourceID string `json:"sourceId"`
}

type apiFetcher func(context.Context, api.Connector, string, time.Duration, int) ([]api.Subscription, error)

// APIConnectors lists the declarative vendor API connectors available to API
// sources.
func (s *SourcesService) APIConnectors() []APIConnector {
	connectors := api.Connectors()
	out := make([]APIConnector, 0, len(connectors))
	for _, connector := range connectors {
		auths := make([]string, 0, len(connector.AuthModes))
		for _, mode := range connector.AuthModes {
			auths = append(auths, string(mode))
		}
		out = append(out, APIConnector{
			ID:      connector.ID,
			Name:    connector.Name,
			Vendor:  connector.Vendor,
			DocsURL: connector.DocsURL,
			Auths:   auths,
		})
	}
	return out
}

func sourceNeedsCredential(src domain.Source) bool {
	switch src.Kind {
	case domain.SourceEmail:
		return true
	case domain.SourceAPI:
		return src.API.Auth == domain.APIAuthKey
	default:
		return false
	}
}

func sourceCredentialLabel(src domain.Source) string {
	switch src.Kind {
	case domain.SourceEmail:
		return "password"
	case domain.SourceAPI:
		return "API key"
	default:
		return "credential"
	}
}

func apiSourceFromInput(base domain.Source, in SourceInput) (domain.Source, error) {
	src := base
	src.Kind = domain.SourceAPI
	src.Name = in.Name
	src.Enabled = in.Enabled
	src.ScheduleMinutes = in.ScheduleMinutes
	src.API = sourceAPIConfig(in.Config)
	src.Normalize()
	if err := src.Validate(); err != nil {
		return domain.Source{}, err
	}
	connector, err := api.ConnectorFor(src.API.Connector)
	if err != nil {
		return domain.Source{}, err
	}
	if !connector.SupportsAuth(src.API.Auth) {
		return domain.Source{}, fmt.Errorf("%s does not support %s authentication", connector.Name, src.API.Auth)
	}
	return src, nil
}

// testAPI fetches a small number of subscriptions without storing the token.
func (s *SourcesService) testAPI(id string, in SourceInput) (SourceTestResult, error) {
	var base domain.Source
	if id != "" {
		stored, err := s.store.GetSource(context.Background(), id)
		if err != nil {
			return SourceTestResult{Error: err.Error()}, nil
		}
		base = stored
	}
	src, err := apiSourceFromInput(base, in)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	token := in.Password
	if token == "" && base.SecretRef != "" {
		value, err := s.secrets.Get(base.SecretRef)
		if err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return SourceTestResult{Error: fmt.Sprintf("load credential: %v", err)}, nil
		}
		token = value
	}
	if token == "" {
		return SourceTestResult{Error: "enter the API key first"}, nil
	}
	connector, err := api.ConnectorFor(src.API.Connector)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	limit := src.API.MaxItems
	if limit > 5 {
		limit = 5
	}
	items, err := s.apiFetch(context.Background(), connector, token,
		time.Duration(src.API.TimeoutSeconds)*time.Second, limit)
	if err != nil {
		return SourceTestResult{Error: err.Error()}, nil
	}
	return SourceTestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected to %s (%d subscriptions found)", connector.Name, len(items)),
	}, nil
}

// APIHandler executes queued vendor API syncs. It is not a bound service.
type APIHandler struct {
	store     *store.Store
	secrets   secrets.Store
	ingestion *IngestionHandler
	fetch     apiFetcher
	notify    func(Source)
	now       func() time.Time
}

// NewAPIHandler wires the API channel to the shared ingestion pipeline.
func NewAPIHandler(st *store.Store, secretStore secrets.Store, ingestion *IngestionHandler) *APIHandler {
	return &APIHandler{
		store:     st,
		secrets:   secretStore,
		ingestion: ingestion,
		fetch: func(ctx context.Context, connector api.Connector, token string, timeout time.Duration, max int) ([]api.Subscription, error) {
			return api.NewClient(timeout).Subscriptions(ctx, connector, token, max)
		},
		now: time.Now,
	}
}

// SetNotifier registers a callback for source:status updates.
func (h *APIHandler) SetNotifier(fn func(Source)) { h.notify = fn }

// RunJob is the jobs.Handler for API syncs.
func (h *APIHandler) RunJob(ctx context.Context, job domain.Job, progress func(int, string)) (string, error) {
	var payload apiPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return "", fmt.Errorf("decode API payload: %w", err)
	}
	payload.SourceID = strings.TrimSpace(payload.SourceID)
	if payload.SourceID == "" {
		return "", fmt.Errorf("API sync payload is missing a source")
	}
	message, err := h.sync(ctx, job.ID, payload.SourceID, progress)
	if err != nil {
		if ctx.Err() == nil {
			h.markError(ctx, payload.SourceID, err)
		}
		return "", err
	}
	return message, nil
}

func (h *APIHandler) sync(ctx context.Context, jobID, sourceID string, progress func(int, string)) (string, error) {
	apiReport(progress, 2, "Loading source")
	src, err := h.store.GetSource(ctx, sourceID)
	if err != nil {
		return "", fmt.Errorf("load source: %w", err)
	}
	if src.Kind != domain.SourceAPI {
		return "", fmt.Errorf("source %q is not an API source", src.Name)
	}
	if !src.Enabled {
		return "", fmt.Errorf("source %q is disabled", src.Name)
	}
	src.API.Normalize()
	if err := src.API.Validate(); err != nil {
		return "", fmt.Errorf("source %q is misconfigured: %w", src.Name, err)
	}
	if src.SecretRef == "" {
		return "", fmt.Errorf("source %q has no API credential", src.Name)
	}
	token, err := h.secrets.Get(src.SecretRef)
	if err != nil {
		return "", fmt.Errorf("load API credential: %w", err)
	}
	connector, err := api.ConnectorFor(src.API.Connector)
	if err != nil {
		return "", fmt.Errorf("source %q: %w", src.Name, err)
	}
	if !connector.SupportsAuth(src.API.Auth) {
		return "", fmt.Errorf("source %q: %s does not support %s authentication", src.Name, connector.Name, src.API.Auth)
	}

	apiReport(progress, 10, "Fetching subscriptions from "+connector.Name)
	items, err := h.fetch(ctx, connector, token,
		time.Duration(src.API.TimeoutSeconds)*time.Second, src.API.MaxItems)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(items) == 0 {
		now := h.now()
		if err := h.store.MarkSourceSynced(ctx, src.ID, now); err != nil {
			return "", err
		}
		src.LastSyncAt = now
		src.LastError = ""
		h.emitSource(src)
		apiReport(progress, 100, "No subscriptions returned")
		return "No subscriptions returned", nil
	}

	pipeline, err := h.ingestion.pipeline(ctx, jobID)
	if err != nil {
		return "", err
	}
	counts := map[string]int{}
	for index, subscription := range items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		percent := 20 + (index*70)/len(items)
		apiReport(progress, percent, fmt.Sprintf("Processing %d of %d", index+1, len(items)))
		proposal := subscription.Proposal
		result, err := pipeline.Run(ctx, ingest.RawItem{
			Channel:    "api",
			ExternalID: subscription.ExternalID,
			Title:      subscription.Title,
			Content:    subscription.Content,
			VendorHint: subscription.VendorHint,
			OccurredAt: subscription.OccurredAt,
			Proposal:   &proposal,
		}, nil)
		if err != nil {
			return "", fmt.Errorf("ingest %s: %w", subscription.ExternalID, err)
		}
		if result.ReviewID != "" {
			h.ingestion.notifyNewReview(ctx, result.ReviewID)
		}
		counts[result.Action]++
	}

	now := h.now()
	if err := h.store.MarkSourceSynced(ctx, src.ID, now); err != nil {
		return "", err
	}
	src.LastSyncAt = now
	src.LastError = ""
	h.emitSource(src)

	summary := apiSummary(len(items), counts)
	apiReport(progress, 100, summary)
	return summary, nil
}

func apiReport(progress func(int, string), percent int, message string) {
	if progress != nil {
		progress(percent, message)
	}
}

func apiSummary(fetched int, counts map[string]int) string {
	parts := []string{fmt.Sprintf("%d subscriptions read", fetched)}
	for _, action := range []string{ingest.ActionCreated, ingest.ActionMatched, ingest.ActionUpdated, ingest.ActionReview, ingest.ActionDuplicate, ingest.ActionIgnored} {
		if counts[action] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[action], action))
		}
	}
	return strings.Join(parts, ", ")
}

func (h *APIHandler) emitSource(src domain.Source) {
	if h.notify != nil {
		h.notify(sourceDTO(src))
	}
}

func (h *APIHandler) markError(ctx context.Context, sourceID string, cause error) {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.store.MarkSourceError(writeCtx, sourceID, message); err != nil {
		return
	}
	if src, err := h.store.GetSource(writeCtx, sourceID); err == nil {
		h.emitSource(src)
	}
}
