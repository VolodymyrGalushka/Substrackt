package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/ingest"
	"substrackt/internal/mail"
	"substrackt/internal/secrets"
	"substrackt/internal/store"
)

// EmailJobKind is the queue kind for one IMAP sync.
const EmailJobKind = "email.sync"

type emailPayload struct {
	SourceID string `json:"sourceId"`
}

// emailClient is the subset of the IMAP client the sync uses. Tests replace it
// through the dialer so no network is required.
type emailClient interface {
	SelectInfo(mailbox string) (mail.MailboxInfo, error)
	FetchSince(mailbox string, lastUID uint32, since time.Time, limit int) (mail.MailboxInfo, []mail.Message, error)
	Abort() error
	Close() error
}

type emailDialer func(mail.Config) (emailClient, error)

func dialIMAP(cfg mail.Config) (emailClient, error) { return mail.Dial(cfg) }

// EmailHandler executes queued IMAP syncs. It is not a bound service: only
// SourcesService is exposed to the frontend.
type EmailHandler struct {
	store     *store.Store
	secrets   secrets.Store
	ingestion *IngestionHandler
	notify    func(Source)
	dial      emailDialer
	now       func() time.Time
}

// NewEmailHandler wires the email channel to the shared ingestion pipeline.
func NewEmailHandler(st *store.Store, secretStore secrets.Store, ingestion *IngestionHandler) *EmailHandler {
	return &EmailHandler{
		store:     st,
		secrets:   secretStore,
		ingestion: ingestion,
		dial:      dialIMAP,
		now:       time.Now,
	}
}

// SetNotifier registers a callback fired after a sync changes source status.
// Main uses it to emit source:status.
func (h *EmailHandler) SetNotifier(fn func(Source)) {
	h.notify = fn
}

// RunJob is the jobs.Handler for email syncs.
func (h *EmailHandler) RunJob(ctx context.Context, job domain.Job, progress func(int, string)) (string, error) {
	var payload emailPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return "", fmt.Errorf("decode email payload: %w", err)
	}
	payload.SourceID = strings.TrimSpace(payload.SourceID)
	if payload.SourceID == "" {
		return "", fmt.Errorf("email sync payload is missing a source")
	}

	message, err := h.sync(ctx, job.ID, payload.SourceID, progress)
	if err != nil {
		// A cancelled job is not a source failure; record real sync errors only.
		if ctx.Err() == nil {
			h.markError(ctx, payload.SourceID, err)
		}
		return "", err
	}
	return message, nil
}

func (h *EmailHandler) sync(ctx context.Context, jobID, sourceID string, progress func(int, string)) (string, error) {
	emailReport(progress, 2, "Loading source")
	src, err := h.store.GetSource(ctx, sourceID)
	if err != nil {
		return "", fmt.Errorf("load source: %w", err)
	}
	if src.Kind != domain.SourceEmail {
		return "", fmt.Errorf("source %q is not an email source", src.Name)
	}
	if !src.Enabled {
		return "", fmt.Errorf("source %q is disabled", src.Name)
	}

	cfg := src.Config
	cfg.Normalize()

	emailReport(progress, 5, "Loading credentials")
	password, err := h.secrets.Get(src.SecretRef)
	if err != nil {
		return "", fmt.Errorf("load password for %q: %w", src.Name, err)
	}

	emailReport(progress, 8, "Connecting to "+cfg.Address)
	client, err := h.dial(mail.Config{
		Address:  cfg.Address,
		Username: cfg.Username,
		Password: password,
		Insecure: cfg.Insecure,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()

	cursor, cursorErr := h.store.GetMailboxCursor(ctx, src.ID, cfg.Mailbox)
	if cursorErr != nil && !errors.Is(cursorErr, store.ErrNotFound) {
		return "", cursorErr
	}
	lastUID := cursor.LastUID
	var since time.Time
	if lastUID == 0 {
		// Bound the first sync so a large mailbox does not flood the queue.
		since = h.now().AddDate(0, 0, -cfg.InitialDays)
	}

	info, messages, err := h.fetch(ctx, client, cfg, lastUID, since)
	if err != nil {
		return "", err
	}
	if cursor.UIDValidity != 0 && info.UIDValidity != 0 && info.UIDValidity != cursor.UIDValidity {
		// The mailbox was recreated, so old UIDs no longer identify the same
		// messages. Rescan the recent window under the new UIDVALIDITY.
		emailReport(progress, 15, "Mailbox was reset; rescanning recent mail")
		lastUID = 0
		since = h.now().AddDate(0, 0, -cfg.InitialDays)
		info, messages, err = h.fetch(ctx, client, cfg, lastUID, since)
		if err != nil {
			return "", err
		}
	}

	pipeline, err := h.ingestion.pipeline(ctx, jobID)
	if err != nil {
		return "", err
	}

	var review, created, matched, ignored, duplicates int
	total := len(messages)
	for i, msg := range messages {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		start := 20 + (i*70)/max(total, 1)
		end := 20 + ((i+1)*70)/max(total, 1)
		result, err := pipeline.Run(ctx, emailItem(src, info, msg), scaledProgress(progress, start, end))
		if err != nil {
			return "", fmt.Errorf("ingest message %d: %w", msg.UID, err)
		}
		if result.ReviewID != "" {
			h.ingestion.notifyNewReview(ctx, result.ReviewID)
		}

		switch result.Action {
		case ingest.ActionReview:
			review++
		case ingest.ActionCreated:
			created++
		case ingest.ActionMatched:
			matched++
		case ingest.ActionIgnored:
			ignored++
		case ingest.ActionDuplicate:
			duplicates++
		}

		// Advance after every successfully processed message, including mail the
		// pipeline deliberately ignored, so one message cannot block the mailbox.
		lastUID = msg.UID
		cursor = domain.MailboxCursor{
			SourceID:    src.ID,
			Mailbox:     cfg.Mailbox,
			UIDValidity: info.UIDValidity,
			LastUID:     lastUID,
		}
		if err := h.store.SaveMailboxCursor(ctx, cursor); err != nil {
			return "", err
		}
	}

	if total == 0 {
		// Record UIDVALIDITY even when nothing matched, so a later mailbox reset
		// is detected, while keeping the current position.
		if err := h.store.SaveMailboxCursor(ctx, domain.MailboxCursor{
			SourceID:    src.ID,
			Mailbox:     cfg.Mailbox,
			UIDValidity: info.UIDValidity,
			LastUID:     lastUID,
		}); err != nil {
			return "", err
		}
	}

	now := h.now()
	if err := h.store.MarkSourceSynced(ctx, src.ID, now); err != nil {
		return "", err
	}
	src.LastSyncAt = now
	src.LastError = ""
	h.notifyStatus(src)

	summary := emailSummary(total, review, created, matched, ignored, duplicates)
	emailReport(progress, 100, summary)
	return summary, nil
}

func (h *EmailHandler) fetch(ctx context.Context, client emailClient, cfg domain.EmailSourceConfig, lastUID uint32, since time.Time) (mail.MailboxInfo, []mail.Message, error) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// The IMAP client has no context support; closing the connection is
			// what unblocks a running fetch.
			_ = client.Abort()
		case <-done:
		}
	}()
	defer close(done)

	info, messages, err := client.FetchSince(cfg.Mailbox, lastUID, since, cfg.MaxMessages)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return mail.MailboxInfo{}, nil, ctxErr
		}
		return mail.MailboxInfo{}, nil, fmt.Errorf("fetch %s: %w", cfg.Mailbox, err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return mail.MailboxInfo{}, nil, ctxErr
	}
	return info, messages, nil
}

func emailItem(src domain.Source, info mail.MailboxInfo, msg mail.Message) ingest.RawItem {
	external := strings.TrimSpace(msg.MessageID)
	if external == "" {
		external = fmt.Sprintf("imap:%s:%d:%d", src.ID, info.UIDValidity, msg.UID)
	}
	title := strings.TrimSpace(msg.Subject)
	content := strings.TrimSpace(msg.Text)
	if content == "" {
		content = title
	} else if title != "" && !strings.HasPrefix(content, title) {
		content = title + "\n\n" + content
	}
	if title == "" {
		title = "Email from " + strings.TrimSpace(msg.From)
	}

	vendorHint := strings.TrimSpace(msg.From)
	// "Acme Billing <billing@acme.com>" is a better vendor hint than the whole
	// header, and the heuristic knows how to strip the address.
	if idx := strings.IndexAny(vendorHint, "<>"); idx > 0 {
		vendorHint = strings.TrimSpace(vendorHint[:idx])
	}
	return ingest.RawItem{
		Channel:    "email",
		ExternalID: external,
		Title:      title,
		Content:    content,
		VendorHint: vendorHint,
		OccurredAt: msg.Date,
	}
}

func emailReport(progress func(int, string), percent int, message string) {
	if progress != nil {
		progress(percent, message)
	}
}

func scaledProgress(progress func(int, string), start, end int) func(int, string) {
	if progress == nil {
		return nil
	}
	return func(percent int, message string) {
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		progress(start+(end-start)*percent/100, message)
	}
}

func emailSummary(total, review, created, matched, ignored, duplicates int) string {
	if total == 0 {
		return "No new mail"
	}
	parts := []string{fmt.Sprintf("%d message(s) synced", total)}
	if created > 0 {
		parts = append(parts, fmt.Sprintf("%d created", created))
	}
	if matched > 0 {
		parts = append(parts, fmt.Sprintf("%d matched", matched))
	}
	if review > 0 {
		parts = append(parts, fmt.Sprintf("%d queued for review", review))
	}
	if ignored > 0 {
		parts = append(parts, fmt.Sprintf("%d ignored", ignored))
	}
	if duplicates > 0 {
		parts = append(parts, fmt.Sprintf("%d duplicate(s)", duplicates))
	}
	return strings.Join(parts, ", ")
}

func (h *EmailHandler) notifyStatus(src domain.Source) {
	if h.notify != nil {
		h.notify(sourceDTO(src))
	}
}

func (h *EmailHandler) markError(ctx context.Context, sourceID string, cause error) {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 500 {
		message = message[:500]
	}
	// A cancelled job's context is already done; status writes still need to
	// land so the UI does not show a stale error forever.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.store.MarkSourceError(writeCtx, sourceID, message); err != nil {
		return
	}
	if src, err := h.store.GetSource(writeCtx, sourceID); err == nil {
		h.notifyStatus(src)
	}
}
