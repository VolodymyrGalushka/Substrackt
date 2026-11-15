package ingest

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"substrackt/internal/domain"
)

var (
	isoCurrencies = `USD|EUR|GBP|CAD|AUD|NZD|JPY|INR|CHF|SEK|NOK|DKK|PLN|BRL|MXN|SGD|HKD|ZAR|CZK|TRY|AED|ILS|KRW|CNY|THB|MYR|IDR|PHP|VND|NGN|EGP|SAR|QAR`

	amountBefore = regexp.MustCompile(`(?i)\b(` + isoCurrencies + `)\b\s*([0-9][0-9., ]{0,18})`)
	amountAfter  = regexp.MustCompile(`([0-9][0-9., ]{0,18}?)\s*\b(` + isoCurrencies + `)\b`)
	amountSymbol = regexp.MustCompile(`([$€£¥₹])\s*([0-9][0-9., ]{0,18})`)

	everyPattern      = regexp.MustCompile(`(?i)\bevery\s+(\d+)\s*(day|week|month|year)s?\b`)
	quarterlyPattern  = regexp.MustCompile(`(?i)\b(quarterly|every quarter)\b`)
	semiannualPattern = regexp.MustCompile(`(?i)\b(semi[- ]?annual|half[- ]?yearly)\b`)
	annualPattern     = regexp.MustCompile(`(?i)\b(annual(ly)?|per year|per annum|yearly|/\s*yr|/year)\b`)
	monthlyPattern    = regexp.MustCompile(`(?i)\b(monthly|per month|/\s*mo|/month)\b`)
	weeklyPattern     = regexp.MustCompile(`(?i)\b(weekly|per week|/\s*wk|/week)\b`)
	biweeklyPattern   = regexp.MustCompile(`(?i)\b(bi[- ]?weekly|fortnightly|every two weeks)\b`)
	dailyPattern      = regexp.MustCompile(`(?i)\b(daily|per day|/\s*day)\b`)

	cancelPattern        = regexp.MustCompile(`(?i)\b(cancel(l)?ed|cancel(l)?ation|terminated|membership (has )?ended|account closed)\b`)
	cancelAnytimePattern = regexp.MustCompile(`(?i)cancel\s+(any\s*time|at\s+any\s+time)`)
	trialPattern         = regexp.MustCompile(`(?i)\b(free trial|trial (ends|period|expires)|your trial)\b`)

	vendorLinePattern  = regexp.MustCompile(`(?im)^\s*(?:from|merchant|vendor|seller|payee|billed by|sold by)\s*[:\-]\s*(.{2,120})$`)
	nameEmailPattern   = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9 .&'_-]{1,60})\s*<[^>@\s]+@[^>]+>`)
	emailDomainPattern = regexp.MustCompile(`(?i)\b[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+)\b`)
	planPattern        = regexp.MustCompile(`(?i)\b(?:plan|tier|membership)\s*[:\-]\s*([A-Za-z0-9][A-Za-z0-9 .+_-]{1,40})`)
	knownPlanPattern   = regexp.MustCompile(`(?i)\b(?:your\s+)?(standard|premium|plus|pro|basic|family|business|enterprise|ultimate|individual|personal|student|duo)\s+plan\b`)

	datePattern    = regexp.MustCompile(`(?i)\b(?:\d{4}-\d{1,2}-\d{1,2}|\d{1,2}[/.]\d{1,2}[/.]\d{2,4}|(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}|\d{1,2}(?:st|nd|rd|th)?\s+(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?,?\s+\d{4})\b`)
	ordinalPattern = regexp.MustCompile(`(?i)(\d+)(st|nd|rd|th)`)

	vendorPrefixPattern = regexp.MustCompile(`(?i)^(?:your\s+)?(?:receipt|invoice|payment|billing|subscription|order|confirmation)\s+(?:from|for|to)\s+`)
	genericLinePattern  = regexp.MustCompile(`(?i)^(receipt|invoice|payment|confirmation|order|thanks|thank you|hello|hi\b|your\b|amount\b|total\b|date\b|invoice #|order #)`)
)

var symbolCurrencies = map[string]string{
	"$": "USD",
	"€": "EUR",
	"£": "GBP",
	"¥": "JPY",
	"₹": "INR",
}

// HeuristicModel marks proposals produced by local rules rather than a model.
const HeuristicModel = "heuristic"

// HeuristicExtract parses a document with local rules. Its confidence is capped
// below the default auto-apply threshold, so a rules-only result always goes to
// the review queue.
func HeuristicExtract(content, vendorHint string, occurred, today domain.Date, baseCurrency string) (Extract, bool) {
	if today.IsZero() {
		today = domain.Today()
	}
	if baseCurrency == "" {
		baseCurrency = "USD"
	}

	vendor := findVendor(content, vendorHint)
	amountMinor, currency, amountRaw, amountOK := findAmount(content)
	if currency == "" {
		currency = baseCurrency
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	scale := domain.ScaleFor(currency)
	if currency != "" && amountOK {
		amountMinor, amountOK = parseAmount(amountRaw, scale)
	}

	interval, explicitInterval := findInterval(content)
	renewal := findRenewalDate(content, today)
	status := findStatus(content)
	plan := findPlan(content)
	name := vendor
	if name == "" {
		name = strings.TrimSpace(vendorHint)
	}

	if vendor == "" && amountMinor == 0 {
		return Extract{}, false
	}

	anchor, next := planTiming(interval, renewal, occurred, today)

	score := 0.10
	if vendor != "" {
		score += 0.28
	}
	if amountOK && amountMinor > 0 {
		score += 0.30
	}
	if explicitInterval {
		score += 0.15
	}
	if !renewal.IsZero() {
		score += 0.10
	}
	if status != domain.StatusActive {
		score += 0.03
	}
	if len(content) > 40 {
		score += 0.04
	}
	if score > 0.85 {
		score = 0.85
	}

	evidence := evidenceAround(content, amountRaw)
	rationale := fmt.Sprintf("Local heuristic parse (vendor=%q, amount=%d %s, cycle=%s)",
		vendor, amountMinor, currency, interval.Label())

	raw, _ := json.Marshal(map[string]any{
		"vendor":         vendor,
		"name":           name,
		"plan":           plan,
		"amount_minor":   amountMinor,
		"currency":       currency,
		"interval_count": interval.Count,
		"interval_unit":  string(interval.Unit),
		"anchor_date":    anchor.String(),
		"next_renewal":   next.String(),
		"status":         string(status),
		"confidence":     score,
	})

	return Extract{
		Proposal: Proposal{
			VendorName:  vendor,
			Name:        name,
			Plan:        plan,
			AmountMinor: amountMinor,
			Currency:    currency,
			Scale:       scale,
			Interval:    interval,
			AnchorDate:  anchor,
			NextRenewal: next,
			Status:      status,
			Confidence:  score,
		},
		Model:     HeuristicModel,
		Rationale: rationale,
		Evidence:  evidence,
		Raw:       string(raw),
	}, true
}

// planTiming turns an optional stated renewal date into an anchor and the next
// renewal. Without a stated date the item's occurrence (or today) is the anchor
// and the next period follows it, which matches how a fresh receipt behaves.
func planTiming(iv domain.Interval, renewal, occurred, today domain.Date) (domain.Date, domain.Date) {
	if iv.Count <= 0 || !iv.Unit.Valid() {
		iv = domain.Interval{Count: 1, Unit: domain.UnitMonth}
	}
	if today.IsZero() {
		today = domain.Today()
	}
	if !renewal.IsZero() {
		next := renewal
		if next.Before(today) {
			next = iv.NextRenewal(renewal, today)
		}
		return renewal, next
	}
	anchor := occurred
	if anchor.IsZero() {
		anchor = today
	}
	next := iv.Add(anchor)
	if next.Before(today) {
		next = iv.NextRenewal(anchor, today)
	}
	return anchor, next
}

func findAmount(content string) (int64, string, string, bool) {
	if m := amountBefore.FindStringSubmatch(content); len(m) == 3 {
		if amount, ok := parseAmount(m[2], domain.ScaleFor(strings.ToUpper(m[1]))); ok {
			return amount, strings.ToUpper(m[1]), m[2], true
		}
	}
	if m := amountAfter.FindStringSubmatch(content); len(m) == 3 {
		if amount, ok := parseAmount(m[1], domain.ScaleFor(strings.ToUpper(m[2]))); ok {
			return amount, strings.ToUpper(m[2]), m[1], true
		}
	}
	if m := amountSymbol.FindStringSubmatch(content); len(m) == 3 {
		currency := symbolCurrencies[m[1]]
		if amount, ok := parseAmount(m[2], domain.ScaleFor(currency)); ok {
			return amount, currency, m[2], true
		}
	}
	return 0, "", "", false
}

// parseAmount converts a human-formatted decimal (1.234,56 or 1,234.56) into
// minor units, using the currency's scale to disambiguate separators.
func parseAmount(raw string, scale int) (int64, bool) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\u00a0", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.Trim(s, ".,")
	if s == "" {
		return 0, false
	}

	lastDot := strings.LastIndexByte(s, '.')
	lastComma := strings.LastIndexByte(s, ',')
	decimal := byte(0)
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastDot > lastComma {
			decimal = '.'
		} else {
			decimal = ','
		}
	case lastDot >= 0:
		if decimalSeparator(s, '.', scale) {
			decimal = '.'
		}
	case lastComma >= 0:
		if decimalSeparator(s, ',', scale) {
			decimal = ','
		}
	}

	if decimal != 0 {
		thousands := byte(',')
		if decimal == ',' {
			thousands = '.'
		}
		s = strings.ReplaceAll(s, string(thousands), "")
		s = strings.ReplaceAll(s, string(decimal), ".")
	} else {
		s = strings.ReplaceAll(s, ",", "")
		s = strings.ReplaceAll(s, ".", "")
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	factor := math.Pow10(scale)
	minor := math.Round(value * factor)
	if minor > math.MaxInt64 || minor < 0 {
		return 0, false
	}
	return int64(minor), true
}

func decimalSeparator(s string, sep byte, scale int) bool {
	idx := strings.LastIndexByte(s, sep)
	if idx < 0 {
		return false
	}
	after := len(s) - idx - 1
	if after == 0 {
		return false
	}
	if after == 3 && scale != 3 {
		return false
	}
	if scale == 0 {
		return false
	}
	return after <= 2 || after == scale
}

func findInterval(content string) (domain.Interval, bool) {
	if m := everyPattern.FindStringSubmatch(content); len(m) == 3 {
		count, err := strconv.Atoi(m[1])
		if err == nil && count > 0 {
			unit := domain.Unit(strings.ToLower(m[2]))
			if unit.Valid() {
				return domain.Interval{Count: count, Unit: unit}, true
			}
		}
	}
	switch {
	case quarterlyPattern.MatchString(content):
		return domain.Interval{Count: 3, Unit: domain.UnitMonth}, true
	case semiannualPattern.MatchString(content):
		return domain.Interval{Count: 6, Unit: domain.UnitMonth}, true
	case biweeklyPattern.MatchString(content):
		return domain.Interval{Count: 2, Unit: domain.UnitWeek}, true
	case annualPattern.MatchString(content):
		return domain.Interval{Count: 1, Unit: domain.UnitYear}, true
	case monthlyPattern.MatchString(content):
		return domain.Interval{Count: 1, Unit: domain.UnitMonth}, true
	case weeklyPattern.MatchString(content):
		return domain.Interval{Count: 1, Unit: domain.UnitWeek}, true
	case dailyPattern.MatchString(content):
		return domain.Interval{Count: 1, Unit: domain.UnitDay}, true
	}
	return domain.Interval{Count: 1, Unit: domain.UnitMonth}, false
}

func findStatus(content string) domain.Status {
	withoutAnytime := cancelAnytimePattern.ReplaceAllString(content, "")
	if cancelPattern.MatchString(withoutAnytime) {
		return domain.StatusCancelled
	}
	if trialPattern.MatchString(content) {
		return domain.StatusTrial
	}
	return domain.StatusActive
}

func findPlan(content string) string {
	if m := planPattern.FindStringSubmatch(content); len(m) == 2 {
		return cleanPhrase(m[1])
	}
	if m := knownPlanPattern.FindStringSubmatch(content); len(m) == 2 {
		return titleCase(m[1])
	}
	return ""
}

func findVendor(content, hint string) string {
	if v := cleanVendor(hint); v != "" {
		return v
	}
	if m := vendorLinePattern.FindStringSubmatch(content); len(m) == 2 {
		if v := cleanVendor(m[1]); v != "" {
			return v
		}
	}
	if m := nameEmailPattern.FindStringSubmatch(content); len(m) == 2 {
		if v := cleanVendor(m[1]); v != "" {
			return v
		}
	}
	if m := emailDomainPattern.FindStringSubmatch(content); len(m) == 2 {
		if v := vendorFromDomain(m[1]); v != "" {
			return v
		}
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if i >= 8 {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" || len(line) < 3 {
			continue
		}
		stripped := vendorPrefixPattern.ReplaceAllString(line, "")
		if stripped != line {
			if v := cleanVendor(stripped); v != "" {
				return v
			}
			continue
		}
		if genericLinePattern.MatchString(line) {
			continue
		}
		if v := cleanVendor(line); v != "" {
			return v
		}
	}
	return ""
}

func cleanVendor(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	if idx := strings.IndexAny(s, "<>"); idx >= 0 {
		s = s[:idx]
	}
	for _, sep := range []string{" - ", " – ", " | "} {
		if idx := strings.Index(s, sep); idx > 0 {
			s = s[:idx]
		}
	}
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".,;:")
	for _, suffix := range []string{" receipt", " invoice", " payment", " billing", " confirmation"} {
		if strings.HasSuffix(strings.ToLower(s), suffix) {
			s = s[:len(s)-len(suffix)]
		}
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:120]
	}
	if s == "" || strings.Contains(s, "@") && !strings.Contains(s, " ") {
		return ""
	}
	if s == strings.ToLower(s) {
		s = titleCase(s)
	}
	return s
}

func vendorFromDomain(domainName string) string {
	parts := strings.Split(strings.ToLower(domainName), ".")
	if len(parts) < 2 {
		return ""
	}
	label := parts[len(parts)-2]
	for _, prefix := range []string{"billing", "mail", "email", "no-reply", "noreply", "notifications", "support", "info", "hello"} {
		if label == prefix && len(parts) >= 3 {
			label = parts[len(parts)-3]
		}
	}
	label = strings.TrimPrefix(label, "www")
	if label == "" {
		return ""
	}
	return titleCase(label)
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'a' && r <= 'z' {
			runes[i] = r - ('a' - 'A')
		}
		break
	}
	return string(runes)
}

func cleanPhrase(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'.`)
	if idx := strings.IndexAny(s, "\n\r"); idx >= 0 {
		s = s[:idx]
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.TrimSpace(s)
}

func evidenceAround(content, needle string) string {
	needle = strings.TrimSpace(needle)
	if needle != "" {
		if idx := strings.Index(content, needle); idx >= 0 {
			lineStart := strings.LastIndexByte(content[:idx], '\n') + 1
			lineEnd := strings.IndexByte(content[idx:], '\n')
			if lineEnd < 0 {
				lineEnd = len(content)
			} else {
				lineEnd += idx
			}
			return truncate(strings.TrimSpace(content[lineStart:lineEnd]), 240)
		}
	}
	return truncate(strings.TrimSpace(content), 240)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}

func findRenewalDate(content string, today domain.Date) domain.Date {
	matches := datePattern.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return domain.Date{}
	}

	type scored struct {
		date  domain.Date
		score int
	}
	var best scored
	for _, loc := range matches {
		raw := content[loc[0]:loc[1]]
		date, ok := parseDateToken(raw, today)
		if !ok {
			continue
		}
		score := 0
		start := loc[0] - 60
		if start < 0 {
			start = 0
		}
		context := strings.ToLower(content[start:loc[0]])
		for _, keyword := range []string{"renew", "next payment", "next charge", "next billing", "due", "billing date", "charges on", "valid until"} {
			if strings.Contains(context, keyword) {
				score += 3
				break
			}
		}
		if !date.Before(today) {
			score += 2
		}
		if score > best.score || (score == best.score && date.After(best.date)) {
			best = scored{date: date, score: score}
		}
	}
	return best.date
}

func parseDateToken(raw string, today domain.Date) (domain.Date, bool) {
	raw = ordinalPattern.ReplaceAllString(raw, "$1")
	raw = strings.ReplaceAll(raw, ",", " ")
	raw = strings.Join(strings.Fields(raw), " ")

	layouts := []string{
		"2006-1-2", "2006-01-02",
		"1/2/2006", "1/2/06", "2/1/2006", "2/1/06",
		"2.1.2006", "2.1.06", "1.2.2006", "1.2.06",
		"Jan 2 2006", "January 2 2006",
		"2 Jan 2006", "2 January 2006",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		date := domain.NewDate(t)
		if date.Year == 0 {
			date.Year = today.Year
		}
		if date.Year < 100 {
			date.Year += 2000
		}
		return date, true
	}
	return domain.Date{}, false
}
