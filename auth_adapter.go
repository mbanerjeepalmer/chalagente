package main

import (
	"context"

	"github.com/mbanerjeepalmer/chalagente/internal/clerkauth"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
)

// storeClerkAdapter implements clerkauth.UserStore against *store.Store.
type storeClerkAdapter struct{ s *store.Store }

func (a *storeClerkAdapter) GetUserIDByClerkID(ctx context.Context, clerkID string) (string, error) {
	u, err := a.s.GetUserByClerkID(ctx, clerkID)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

func (a *storeClerkAdapter) EnsureUserByClerk(ctx context.Context, clerkID, email string) (string, error) {
	u, err := a.s.EnsureUserByClerk(ctx, clerkID, email)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

var _ clerkauth.UserStore = (*storeClerkAdapter)(nil)
