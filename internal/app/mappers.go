package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"substrackt/internal/domain"
)

// emit sends a frontend event when the application is running. In tests there
// is no Wails app instance and the event is dropped.
func emit(name string, data ...any) {
	app := application.Get()
	if app == nil || app.Event == nil {
		return
	}
	app.Event.Emit(name, data...)
}

func moneyDTO(m domain.Money) Money {
	return Money{AmountMinor: m.AmountMinor, Currency: m.Currency, Scale: m.Scale}
}

func moneyDomain(m Money) domain.Money {
	return domain.Money{AmountMinor: m.AmountMinor, Currency: m.Currency, Scale: m.Scale}
}

func dateString(d domain.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func datePtrString(d *domain.Date) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func subscriptionDTO(sub domain.Subscription, vendorName, paymentLabel string) Subscription {
	tags := sub.Tags
	if tags == nil {
		tags = []string{}
	}
	return Subscription{
		ID:                 sub.ID,
		Name:               sub.Name,
		Plan:               sub.Plan,
		VendorID:           sub.VendorID,
		VendorName:         vendorName,
		Status:             string(sub.Status),
		Price:              moneyDTO(sub.Price),
		IntervalCount:      sub.Interval.Count,
		IntervalUnit:       string(sub.Interval.Unit),
		AnchorDate:         dateString(sub.AnchorDate),
		NextRenewal:        dateString(sub.NextRenewal),
		StartedAt:          datePtrString(sub.StartedAt),
		TrialEndsAt:        datePtrString(sub.TrialEndsAt),
		CancelledAt:        datePtrString(sub.CancelledAt),
		AutoRenew:          sub.AutoRenew,
		PaymentMethodID:    sub.PaymentMethodID,
		PaymentMethodLabel: paymentLabel,
		Tags:               tags,
		Notes:              sub.Notes,
		CreatedAt:          timeString(sub.CreatedAt),
		UpdatedAt:          timeString(sub.UpdatedAt),
	}
}

func paymentMethodDTO(pm domain.PaymentMethod) PaymentMethod {
	return PaymentMethod{
		ID:              pm.ID,
		Label:           pm.Label,
		Type:            string(pm.Type),
		Issuer:          pm.Issuer,
		Network:         pm.Network,
		Last4:           pm.Last4,
		ExpMonth:        pm.ExpMonth,
		ExpYear:         pm.ExpYear,
		WalletAddress:   pm.WalletAddress,
		Chain:           pm.Chain,
		BillingCurrency: pm.BillingCurrency,
		Active:          pm.Active,
		Notes:           pm.Notes,
		CreatedAt:       timeString(pm.CreatedAt),
		UpdatedAt:       timeString(pm.UpdatedAt),
	}
}

func vendorDTO(v domain.Vendor) Vendor {
	domains := v.Domains
	if domains == nil {
		domains = []string{}
	}
	aliases := v.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return Vendor{
		ID:        v.ID,
		Name:      v.Name,
		Category:  v.Category,
		Domains:   domains,
		Aliases:   aliases,
		LogoURL:   v.LogoURL,
		CreatedAt: timeString(v.CreatedAt),
		UpdatedAt: timeString(v.UpdatedAt),
	}
}
