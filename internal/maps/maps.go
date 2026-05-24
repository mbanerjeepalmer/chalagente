// Package maps wraps Google Maps Places lookups behind a Client interface so
// the rest of the app can talk to a real Google client, a mock, or any future
// provider without caring which.
package maps

import (
	"context"
	"errors"
)

// Place is the slimmed-down view of a Google Maps place that the agent and
// onboarding flow care about. Hours is pre-formatted as a human-readable
// string so the agent can drop it straight into a reply.
type Place struct {
	PlaceID    string
	Name       string
	Address    string
	Phone      string
	Website    string
	Hours      string   // pre-formatted, e.g. "Lun-Vie 9:00-18:00\nSáb 10:00-14:00"
	Categories []string // e.g. ["restaurant", "mexican_restaurant"]
	Lat, Lng   float64
}

// Client is the abstraction over a Maps backend.
type Client interface {
	// Search returns places matching a free-text query.
	Search(ctx context.Context, query string) ([]Place, error)
	// Get fetches full details for a single place by its provider-specific id.
	Get(ctx context.Context, placeID string) (Place, error)
}

// ErrNoAPIKey is returned by GoogleClient when no API key is configured.
var ErrNoAPIKey = errors.New("maps: no API key configured")

// ErrNotFound is returned when a place id is not known.
var ErrNotFound = errors.New("maps: place not found")
