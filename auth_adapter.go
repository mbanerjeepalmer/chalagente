package main

import (
	"context"
	"errors"
	"time"

	"github.com/mbanerjeepalmer/chalagente/internal/auth"
	"github.com/mbanerjeepalmer/chalagente/internal/store"
)

type storeAuthAdapter struct{ s *store.Store }

func (a *storeAuthAdapter) EnsureUserFromCognito(ctx context.Context, sub, email string) (string, error) {
	u, err := a.s.EnsureUserFromCognito(ctx, sub, email)
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

func (a *storeAuthAdapter) CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, time.Time, error) {
	sess, err := a.s.CreateSession(ctx, userID, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return sess.ID, sess.ExpiresAt, nil
}

func (a *storeAuthAdapter) GetSessionUser(ctx context.Context, sessionID string) (string, error) {
	sess, err := a.s.GetSession(ctx, sessionID)
	if err != nil {
		return "", translateErr(err)
	}
	return sess.UserID, nil
}

func (a *storeAuthAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	return a.s.DeleteSession(ctx, sessionID)
}

func translateErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}
