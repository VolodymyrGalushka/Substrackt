package ingest

import "strings"

// Classification is the cheap first pass over an email. It only decides whether
// a message is worth sending to extraction; the extractor still decides whether
// it actually describes a subscription.
type Classification int

const (
	// ClassUnknown means there is not enough signal either way; extract it.
	ClassUnknown Classification = iota
	// ClassBilling means the message looks financial and should be extracted.
	ClassBilling
	// ClassIgnore means the message is clearly not a billing email.
	ClassIgnore
)

var (
	strongBillingTerms = []string{
		"invoice", "receipt", "subscription", "renewal", "renews", "billing",
		"charged", "charge", "amount due", "payment", "transaction",
		"auto-renew", "auto renew",
	}
	weakBillingTerms = []string{
		"price", "plan", "membership", "purchase", "order", "trial",
		"refund", "cancel", "upgrade", "downgrade",
	}
	nonBillingPatterns = []string{
		"newsletter", "weekly digest", "daily digest", "roundup",
		"top stories", "breaking news", "webinar", "event reminder",
		"meeting invitation", "commented on", "mentioned you", "followed you",
		"new follower", "liked your", "password reset", "reset your password",
		"verify your email", "security alert", "new sign-in", "new login",
		"shipping confirmation", "has shipped", "out for delivery",
		"tracking number", "delivery update", "order shipped",
		"discount code", "sale ends", "limited time offer",
	}
)

// Classify looks for billing vocabulary in the subject, sender and the first
// part of the body. Negative signals win over weak billing words, so a shipping
// notice that mentions an "order" is ignored, while a receipt that merely has an
// unsubscribe footer in the body is still billed.
func Classify(subject, from, content string) Classification {
	text := strings.ToLower(subject + "\n" + from + "\n" + truncate(content, 8*1024))
	if containsAny(text, strongBillingTerms) {
		return ClassBilling
	}
	if containsAny(text, nonBillingPatterns) {
		return ClassIgnore
	}
	if containsAny(text, weakBillingTerms) {
		return ClassBilling
	}
	return ClassUnknown
}

func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
