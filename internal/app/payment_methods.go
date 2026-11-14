package app

import (
	"context"
	"fmt"
	"strings"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// PaymentMethodsService exposes payment method CRUD to the frontend. Only
// non-sensitive metadata is stored: never a full card number or CVV.
type PaymentMethodsService struct {
	store *store.Store
}

// NewPaymentMethodsService wires the service to the store.
func NewPaymentMethodsService(st *store.Store) *PaymentMethodsService {
	return &PaymentMethodsService{store: st}
}

// List returns payment methods, including archived ones when requested.
func (s *PaymentMethodsService) List(includeInactive bool) ([]PaymentMethod, error) {
	methods, err := s.store.ListPaymentMethods(context.Background(), includeInactive)
	if err != nil {
		return nil, err
	}
	out := make([]PaymentMethod, 0, len(methods))
	for _, pm := range methods {
		out = append(out, paymentMethodDTO(pm))
	}
	return out, nil
}

// Create adds a payment method.
func (s *PaymentMethodsService) Create(in PaymentMethodInput) (PaymentMethod, error) {
	pm, err := paymentMethodFromInput(domain.PaymentMethod{Active: true}, in)
	if err != nil {
		return PaymentMethod{}, err
	}
	if err := s.store.CreatePaymentMethod(context.Background(), &pm); err != nil {
		return PaymentMethod{}, err
	}
	dto := paymentMethodDTO(pm)
	emit("payment-method:changed", dto)
	return dto, nil
}

// Update replaces the editable fields of a payment method.
func (s *PaymentMethodsService) Update(id string, in PaymentMethodInput) (PaymentMethod, error) {
	ctx := context.Background()
	current, err := s.store.GetPaymentMethod(ctx, id)
	if err != nil {
		return PaymentMethod{}, err
	}
	pm, err := paymentMethodFromInput(current, in)
	if err != nil {
		return PaymentMethod{}, err
	}
	if err := s.store.UpdatePaymentMethod(ctx, &pm); err != nil {
		return PaymentMethod{}, err
	}
	dto := paymentMethodDTO(pm)
	emit("payment-method:changed", dto)
	return dto, nil
}

// SetActive archives or restores a payment method.
func (s *PaymentMethodsService) SetActive(id string, active bool) (PaymentMethod, error) {
	ctx := context.Background()
	pm, err := s.store.GetPaymentMethod(ctx, id)
	if err != nil {
		return PaymentMethod{}, err
	}
	pm.Active = active
	if err := s.store.UpdatePaymentMethod(ctx, &pm); err != nil {
		return PaymentMethod{}, err
	}
	dto := paymentMethodDTO(pm)
	emit("payment-method:changed", dto)
	return dto, nil
}

// Delete removes a payment method; subscriptions using it lose the reference.
func (s *PaymentMethodsService) Delete(id string) error {
	if err := s.store.DeletePaymentMethod(context.Background(), id); err != nil {
		return err
	}
	emit("payment-method:changed", map[string]string{"id": id, "deleted": "true"})
	return nil
}

func paymentMethodFromInput(base domain.PaymentMethod, in PaymentMethodInput) (domain.PaymentMethod, error) {
	pm := base
	pm.Label = in.Label
	pm.Type = domain.PaymentType(strings.ToLower(strings.TrimSpace(in.Type)))
	if pm.Type == "" {
		pm.Type = domain.PaymentCard
	}
	pm.Issuer = in.Issuer
	pm.Network = in.Network
	pm.Last4 = in.Last4
	pm.ExpMonth = in.ExpMonth
	pm.ExpYear = in.ExpYear
	pm.WalletAddress = in.WalletAddress
	pm.Chain = in.Chain
	pm.BillingCurrency = in.BillingCurrency
	pm.Notes = in.Notes
	pm.Active = in.Active

	pm.Normalize()
	if err := pm.Validate(); err != nil {
		return domain.PaymentMethod{}, fmt.Errorf("payment method: %w", err)
	}
	return pm, nil
}
