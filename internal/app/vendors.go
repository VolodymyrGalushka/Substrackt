package app

import (
	"context"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// VendorsService exposes merchant CRUD to the frontend.
type VendorsService struct {
	store *store.Store
}

// NewVendorsService wires the service to the store.
func NewVendorsService(st *store.Store) *VendorsService {
	return &VendorsService{store: st}
}

// List returns all vendors sorted by name.
func (s *VendorsService) List() ([]Vendor, error) {
	vendors, err := s.store.ListVendors(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]Vendor, 0, len(vendors))
	for _, v := range vendors {
		out = append(out, vendorDTO(v))
	}
	return out, nil
}

// Create adds a vendor.
func (s *VendorsService) Create(in VendorInput) (Vendor, error) {
	v := domain.Vendor{
		Name:     in.Name,
		Category: in.Category,
		Domains:  in.Domains,
		Aliases:  in.Aliases,
		LogoURL:  in.LogoURL,
	}
	v.Normalize()
	if err := v.Validate(); err != nil {
		return Vendor{}, err
	}
	if err := s.store.CreateVendor(context.Background(), &v); err != nil {
		return Vendor{}, err
	}
	emit("vendor:changed", vendorDTO(v))
	return vendorDTO(v), nil
}

// Update replaces the editable fields of a vendor.
func (s *VendorsService) Update(id string, in VendorInput) (Vendor, error) {
	ctx := context.Background()
	v, err := s.store.GetVendor(ctx, id)
	if err != nil {
		return Vendor{}, err
	}
	v.Name = in.Name
	v.Category = in.Category
	v.Domains = in.Domains
	v.Aliases = in.Aliases
	v.LogoURL = in.LogoURL
	v.Normalize()
	if err := v.Validate(); err != nil {
		return Vendor{}, err
	}
	if err := s.store.UpdateVendor(ctx, &v); err != nil {
		return Vendor{}, err
	}
	emit("vendor:changed", vendorDTO(v))
	return vendorDTO(v), nil
}

// Delete removes a vendor; subscriptions keep their data but lose the link.
func (s *VendorsService) Delete(id string) error {
	if err := s.store.DeleteVendor(context.Background(), id); err != nil {
		return err
	}
	emit("vendor:changed", map[string]string{"id": id, "deleted": "true"})
	return nil
}
