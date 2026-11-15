package app

import (
	"context"
	"time"

	"substrackt/internal/domain"
	"substrackt/internal/store"
)

// appToday returns the user's current calendar date, honouring the timezone in
// settings. Billing dates are calendar dates, so the timezone only decides which
// day it is, never a timestamp.
func appToday(ctx context.Context, st *store.Store) domain.Date {
	settings, err := st.GetSettings(ctx)
	if err == nil && settings.Timezone != "" {
		if loc, err := time.LoadLocation(settings.Timezone); err == nil {
			return domain.NewDate(time.Now().In(loc))
		}
	}
	return domain.Today()
}

func (s *SubscriptionsService) today(ctx context.Context) domain.Date {
	return appToday(ctx, s.store)
}
