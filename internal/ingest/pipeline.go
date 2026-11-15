package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/llm"
	"substrackt/internal/store"
)

// Pipeline runs Fetch→Normalize→Dedupe→Classify→Extract→Match→Apply for one
// RawItem. Channels differ only in how they produce the item.
type Pipeline struct {
	Store   *store.Store
	Applier Applier
	Config  Config
}

// Run ingests one item and reports progress through the optional callback.
func (p *Pipeline) Run(ctx context.Context, item RawItem, progress func(int, string)) (Result, error) {
	if p.Store == nil || p.Applier == nil {
		return Result{}, fmt.Errorf("pipeline is not configured")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	report(progress, 5, "Normalising content")
	content := NormalizeText(item.Content)
	title := strings.TrimSpace(item.Title)
	vendorHint := strings.TrimSpace(item.VendorHint)
	if vendorHint == "" {
		vendorHint = title
	}
	if content == "" && title == "" {
		return Result{}, fmt.Errorf("nothing to ingest")
	}
	channel := strings.TrimSpace(item.Channel)
	if channel == "" {
		channel = "manual"
	}

	hash := ContentHash(content)
	artifact := domain.Artifact{
		Kind:        channel,
		MediaType:   "text/plain",
		ContentHash: hash,
		Content:     content,
	}
	// existingResult reports a duplicate when the artifact already has a
	// proposal. It returns false when the content must be (re)processed.
	existingResult := func(existing domain.Artifact) (Result, bool, error) {
		extraction, exErr := p.Store.LatestExtractionByArtifact(ctx, existing.ID)
		if exErr == nil {
			result := Result{
				Action:       ActionDuplicate,
				ArtifactID:   existing.ID,
				ExtractionID: extraction.ID,
			}
			if extraction.Status == domain.ExtractionPending {
				result.ReviewID = extraction.ID
				result.Message = "Identical content is already waiting for review"
			} else {
				result.Message = "Identical content was already ingested"
			}
			if extraction.MatchSubID != "" {
				result.SubscriptionID = extraction.MatchSubID
			}
			report(progress, 100, result.Message)
			return result, true, nil
		}
		if !errors.Is(exErr, store.ErrNotFound) {
			return Result{}, false, exErr
		}
		return Result{}, false, nil
	}

	existing, err := p.Store.FindArtifactByHash(ctx, hash)
	switch {
	case err == nil:
		if result, duplicate, exErr := existingResult(existing); exErr != nil {
			return Result{}, exErr
		} else if duplicate {
			return result, nil
		}
		// The content was stored before but produced no proposal (for example
		// it was ignored by the rules or the parse failed). Reprocess it rather
		// than treating it as an unchangeable duplicate.
		artifact = existing
	case errors.Is(err, store.ErrNotFound):
		metadata, _ := json.Marshal(map[string]string{
			"channel":     channel,
			"title":       title,
			"external_id": item.ExternalID,
		})
		artifact.Metadata = string(metadata)
		if err := p.Store.CreateArtifact(ctx, &artifact); err != nil {
			// Another worker may have inserted the same content between the
			// lookup and the insert. Treat it as a duplicate instead of
			// failing the job.
			concurrent, findErr := p.Store.FindArtifactByHash(ctx, hash)
			if findErr != nil {
				return Result{}, err
			}
			if result, duplicate, exErr := existingResult(concurrent); exErr != nil {
				return Result{}, exErr
			} else if duplicate {
				return result, nil
			}
			result := Result{
				Action:     ActionDuplicate,
				ArtifactID: concurrent.ID,
				Message:    "Identical content was ingested concurrently",
			}
			report(progress, 100, result.Message)
			return result, nil
		}
	default:
		return Result{}, err
	}

	report(progress, 20, "Classifying")
	result := Result{ArtifactID: artifact.ID}
	today := p.Config.Today
	if today.IsZero() {
		today = domain.Today()
	}
	occurred := item.OccurredDate()

	var (
		ex      Extract
		llmErr  error
		llmUsed bool
	)
	if p.Config.Client != nil {
		model := p.Config.Model
		if model == "" {
			model = p.Config.Client.Model()
		}
		report(progress, 30, "Extracting with "+model)
		var isSubscription bool
		ex, isSubscription, llmErr = p.extractWithLLM(ctx, content, occurred, today)
		llmUsed = true
		if llmErr == nil && !isSubscription {
			result.Action = ActionIgnored
			result.Message = "Not recognised as a subscription"
			report(progress, 100, result.Message)
			return result, nil
		}
	}

	if !llmUsed || llmErr != nil {
		if llmErr != nil {
			report(progress, 40, "LLM unavailable; using local rules")
		} else {
			report(progress, 40, "Extracting with local rules")
		}
		heuristic, ok := HeuristicExtract(content, vendorHint, occurred, today, p.Config.BaseCurrency)
		if !ok {
			if llmErr != nil {
				return Result{}, fmt.Errorf("LLM extraction failed: %w", llmErr)
			}
			result.Action = ActionIgnored
			result.Message = "No subscription details found"
			report(progress, 100, result.Message)
			return result, nil
		}
		ex = heuristic
		if llmErr != nil {
			ex.Rationale = strings.TrimSpace(ex.Rationale +
				fmt.Sprintf(" (LLM unavailable: %s)", llmErr.Error()))
		}
	}

	ex = normalizeExtract(ex, p.Config.BaseCurrency)
	if ex.VendorName == "" && ex.Name == "" {
		result.Action = ActionIgnored
		result.Message = "No subscription details found"
		report(progress, 100, result.Message)
		return result, nil
	}

	extraction := domain.Extraction{
		JobID:         p.Config.JobID,
		ArtifactID:    artifact.ID,
		Source:        channel,
		Status:        domain.ExtractionPending,
		VendorName:    ex.VendorName,
		Name:          ex.Name,
		Plan:          ex.Plan,
		AmountMinor:   ex.AmountMinor,
		Currency:      ex.Currency,
		Scale:         ex.Scale,
		Interval:      ex.Interval,
		AnchorDate:    ex.AnchorDate,
		NextRenewal:   ex.NextRenewal,
		DesiredStatus: ex.Status,
		PaymentHint:   ex.PaymentHint,
		Confidence:    ex.Confidence,
		Model:         ex.Model,
		Rationale:     ex.Rationale,
		Evidence:      ex.Evidence,
		Raw:           ex.Raw,
	}
	if err := p.Store.CreateExtraction(ctx, &extraction); err != nil {
		return Result{}, err
	}
	result.ExtractionID = extraction.ID

	report(progress, 60, "Matching existing subscriptions")
	match, err := p.Applier.FindMatch(ctx, ex.Proposal)
	if err != nil {
		return Result{}, err
	}
	extraction.MatchKind = match.Kind
	extraction.MatchSubID = match.SubscriptionID

	provenance := Provenance{
		Source:     channel,
		ExternalID: strings.TrimSpace(item.ExternalID),
		ArtifactID: artifact.ID,
		JobID:      p.Config.JobID,
		Confidence: ex.Confidence,
	}
	if provenance.ExternalID == "" {
		provenance.ExternalID = artifact.ContentHash
	}

	switch match.Kind {
	case domain.MatchSame:
		extraction.Status = domain.ExtractionAccepted
		extraction.MatchSubID = match.SubscriptionID
		extraction.ResolvedAt = time.Now()
		result.Action = ActionMatched
		result.SubscriptionID = match.SubscriptionID
		result.Message = fmt.Sprintf("Matched existing subscription %q", match.SubscriptionName)

	case domain.MatchPriceChange, domain.MatchCancellation:
		result.Action = ActionReview
		result.ReviewID = extraction.ID
		result.Message = fmt.Sprintf("%s queued for review: %s", ex.Name, match.Reason)

	default:
		if p.canAutoApply(ex, match) {
			report(progress, 80, "Creating subscription")
			subID, err := p.Applier.CreateProposal(ctx, ex.Proposal, provenance)
			if err != nil {
				extraction.Error = err.Error()
				result.Action = ActionReview
				result.ReviewID = extraction.ID
				result.Message = fmt.Sprintf("Automatic apply failed, queued for review: %v", err)
				break
			}
			extraction.Status = domain.ExtractionAccepted
			extraction.MatchSubID = subID
			extraction.ResolvedAt = time.Now()
			result.Action = ActionCreated
			result.SubscriptionID = subID
			result.Message = fmt.Sprintf("Created subscription %q automatically", ex.Name)
		} else {
			reason := "needs review"
			if match.Kind == domain.MatchAmbiguous {
				reason = match.Reason
			}
			result.Action = ActionReview
			result.ReviewID = extraction.ID
			result.Message = fmt.Sprintf("%s queued for review: %s", ex.Name, reason)
		}
	}

	if err := p.Store.UpdateExtraction(ctx, &extraction); err != nil {
		return Result{}, err
	}
	result.Message = strings.TrimSpace(result.Message)
	report(progress, 100, result.Message)
	return result, nil
}

func (p *Pipeline) today() domain.Date {
	if !p.Config.Today.IsZero() {
		return p.Config.Today
	}
	return domain.Today()
}

func (p *Pipeline) canAutoApply(ex Extract, match Match) bool {
	if !p.Config.AutoApply {
		return false
	}
	if match.Kind != domain.MatchNone {
		return false
	}
	if ex.Confidence < p.Config.Threshold {
		return false
	}
	// Only a model extraction may auto-apply. Local rules are a fallback and
	// stay below review regardless of a user-lowered threshold.
	if ex.Model == "" || ex.Model == HeuristicModel {
		return false
	}
	if strings.TrimSpace(ex.VendorName) == "" {
		return false
	}
	if ex.AmountMinor <= 0 || strings.TrimSpace(ex.Currency) == "" {
		return false
	}
	if ex.Status == domain.StatusCancelled {
		// Cancellations always need a human decision.
		return false
	}
	if ex.AnchorDate.IsZero() {
		return false
	}
	return ex.Interval.Valid() == nil
}

func normalizeExtract(ex Extract, baseCurrency string) Extract {
	ex.VendorName = strings.TrimSpace(ex.VendorName)
	ex.Name = strings.TrimSpace(ex.Name)
	ex.Plan = strings.TrimSpace(ex.Plan)
	if ex.Name == "" {
		ex.Name = ex.VendorName
	}
	if ex.VendorName == "" {
		ex.VendorName = ex.Name
	}
	ex.Currency = strings.ToUpper(strings.TrimSpace(ex.Currency))
	if ex.Currency == "" {
		ex.Currency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	}
	if ex.Currency == "" {
		ex.Currency = "USD"
	}
	if ex.Scale < 0 || ex.Scale > 8 {
		ex.Scale = domain.ScaleFor(ex.Currency)
	}
	if ex.Interval.Count <= 0 {
		ex.Interval.Count = 1
	}
	if !ex.Interval.Unit.Valid() {
		ex.Interval.Unit = domain.UnitMonth
	}
	if ex.Status == "" || !ex.Status.Valid() {
		ex.Status = domain.StatusActive
	}
	if ex.Confidence < 0 {
		ex.Confidence = 0
	}
	if ex.Confidence > 1 {
		ex.Confidence = 1
	}
	return ex
}

func (p *Pipeline) extractWithLLM(ctx context.Context, content string, occurred, today domain.Date) (Extract, bool, error) {
	model := p.Config.Model
	if model == "" && p.Config.Client != nil {
		model = p.Config.Client.Model()
	}
	key := cacheKey(model, content)

	if cached, found, err := p.Store.GetLLMCache(ctx, key); err == nil && found {
		var out llm.Extraction
		if json.Unmarshal([]byte(cached), &out) == nil {
			out.Normalize()
			if out.Validate() == nil {
				p.logUsage(ctx, model, 0, 0, 0, true, true, nil)
				if !out.IsSubscription {
					return Extract{}, false, nil
				}
				ex, err := p.fromLLM(out, occurred, today)
				return ex, true, err
			}
		}
	}

	started := time.Now()
	out, meta, err := p.Config.Client.Extract(ctx, content)
	latency := time.Since(started)
	if err != nil {
		p.logUsage(ctx, model, meta.PromptTokens, meta.CompletionTokens, latency, false, false, err)
		return Extract{}, false, err
	}
	p.logUsage(ctx, model, meta.PromptTokens, meta.CompletionTokens, latency, true, false, nil)

	if encoded, err := json.Marshal(out); err == nil {
		_ = p.Store.PutLLMCache(ctx, key, model, string(encoded))
	}
	if !out.IsSubscription {
		return Extract{}, false, nil
	}
	ex, err := p.fromLLM(out, occurred, today)
	return ex, true, err
}

func (p *Pipeline) fromLLM(out llm.Extraction, occurred, today domain.Date) (Extract, error) {
	currency := strings.ToUpper(strings.TrimSpace(out.Currency))
	if currency == "" {
		currency = strings.ToUpper(strings.TrimSpace(p.Config.BaseCurrency))
	}
	if currency == "" {
		currency = "USD"
	}
	scale := domain.ScaleFor(currency)

	var amountMinor int64
	if strings.TrimSpace(out.Amount) != "" {
		parsed, ok := parseAmount(out.Amount, scale)
		if !ok {
			return Extract{}, fmt.Errorf("LLM returned an unparseable amount %q", out.Amount)
		}
		amountMinor = parsed
	}

	interval := domain.Interval{
		Count: out.IntervalCount,
		Unit:  domain.Unit(strings.ToLower(out.IntervalUnit)),
	}
	if interval.Count <= 0 || !interval.Unit.Valid() {
		return Extract{}, fmt.Errorf("LLM returned an invalid billing interval")
	}

	var renewal domain.Date
	if out.NextRenewal != "" {
		parsed, err := domain.ParseDate(out.NextRenewal)
		if err != nil {
			return Extract{}, err
		}
		renewal = parsed
	}

	status := domain.StatusActive
	switch out.Status {
	case "trial":
		status = domain.StatusTrial
	case "cancelled":
		status = domain.StatusCancelled
	}

	anchor, next := planTiming(interval, renewal, occurred, today)
	model := p.Config.Model
	if model == "" && p.Config.Client != nil {
		model = p.Config.Client.Model()
	}
	raw, _ := json.Marshal(out)
	return Extract{
		Proposal: Proposal{
			VendorName:  out.Vendor,
			Name:        out.Name,
			Plan:        out.Plan,
			AmountMinor: amountMinor,
			Currency:    currency,
			Scale:       scale,
			Interval:    interval,
			AnchorDate:  anchor,
			NextRenewal: next,
			Status:      status,
			Confidence:  out.Confidence,
			PaymentHint: out.PaymentHint,
		},
		Model:     model,
		Rationale: out.Reasoning,
		Evidence:  out.Evidence,
		Raw:       string(raw),
	}, nil
}

func (p *Pipeline) logUsage(ctx context.Context, model string, prompt, completion int, latency time.Duration, ok, cached bool, err error) {
	baseURL := ""
	if p.Config.Client != nil {
		baseURL = p.Config.Client.BaseURL()
	}
	usage := &domain.LLMUsage{
		Model:            model,
		BaseURL:          baseURL,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		LatencyMS:        latency.Milliseconds(),
		OK:               ok,
		Cached:           cached,
	}
	if err != nil {
		usage.Error = err.Error()
	}
	_ = p.Store.LogLLMUsage(ctx, usage)
}

func cacheKey(model, content string) string {
	return ContentHash(model + "\x00" + llm.PromptVersion + "\x00" + content)
}
