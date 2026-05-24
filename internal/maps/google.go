package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultBaseURL is the production Places API (New) host.
const defaultBaseURL = "https://places.googleapis.com"

// fieldMask lists the Place fields we ask Google to populate. Google rejects
// requests without an X-Goog-FieldMask, and you pay per field, so keep this
// in sync with what Place actually uses.
//
// For searchText responses the fields are nested under "places."; for a
// single-place GET the same names are used without that prefix.
var (
	searchFieldMask = strings.Join([]string{
		"places.id",
		"places.displayName",
		"places.formattedAddress",
		"places.internationalPhoneNumber",
		"places.websiteUri",
		"places.regularOpeningHours",
		"places.types",
		"places.location",
	}, ",")

	getFieldMask = strings.Join([]string{
		"id",
		"displayName",
		"formattedAddress",
		"internationalPhoneNumber",
		"websiteUri",
		"regularOpeningHours",
		"types",
		"location",
	}, ",")
)

// GoogleClient is a thin HTTP client over the Google Places API (New).
// Zero value is unusable — APIKey must be set or every call returns
// ErrNoAPIKey. BaseURL and HTTPClient default to sensible production values
// when left zero so tests can inject an httptest server.
type GoogleClient struct {
	APIKey     string
	HTTPClient *http.Client
	BaseURL    string // default "https://places.googleapis.com"
}

// googlePlace mirrors the subset of fields we ask Google for. Only used to
// decode JSON; convert to our Place via toPlace.
type googlePlace struct {
	ID               string `json:"id"`
	DisplayName      struct {
		Text         string `json:"text"`
		LanguageCode string `json:"languageCode"`
	} `json:"displayName"`
	FormattedAddress         string `json:"formattedAddress"`
	InternationalPhoneNumber string `json:"internationalPhoneNumber"`
	WebsiteURI               string `json:"websiteUri"`
	RegularOpeningHours      struct {
		WeekdayDescriptions []string `json:"weekdayDescriptions"`
	} `json:"regularOpeningHours"`
	Types    []string `json:"types"`
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
}

func (g googlePlace) toPlace() Place {
	return Place{
		PlaceID:    g.ID,
		Name:       g.DisplayName.Text,
		Address:    g.FormattedAddress,
		Phone:      g.InternationalPhoneNumber,
		Website:    g.WebsiteURI,
		Hours:      strings.Join(g.RegularOpeningHours.WeekdayDescriptions, "\n"),
		Categories: g.Types,
		Lat:        g.Location.Latitude,
		Lng:        g.Location.Longitude,
	}
}

func (c *GoogleClient) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return c.BaseURL
}

func (c *GoogleClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Search calls POST /v1/places:searchText with the free-text query.
func (c *GoogleClient) Search(ctx context.Context, query string) ([]Place, error) {
	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}

	body, err := json.Marshal(map[string]any{
		"textQuery": query,
	})
	if err != nil {
		return nil, fmt.Errorf("maps: marshal search body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/v1/places:searchText", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("maps: build search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.APIKey)
	req.Header.Set("X-Goog-FieldMask", searchFieldMask)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("maps: search: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("maps: read search response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("maps: search: http %d: %s", resp.StatusCode, string(raw))
	}

	var decoded struct {
		Places []googlePlace `json:"places"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("maps: decode search response: %w", err)
	}

	out := make([]Place, 0, len(decoded.Places))
	for _, gp := range decoded.Places {
		out = append(out, gp.toPlace())
	}
	return out, nil
}

// Get calls GET /v1/places/{placeID}. A 404 from Google maps to ErrNotFound
// so callers can react without sniffing string error messages.
func (c *GoogleClient) Get(ctx context.Context, placeID string) (Place, error) {
	if c.APIKey == "" {
		return Place{}, ErrNoAPIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL()+"/v1/places/"+placeID, nil)
	if err != nil {
		return Place{}, fmt.Errorf("maps: build get request: %w", err)
	}
	req.Header.Set("X-Goog-Api-Key", c.APIKey)
	req.Header.Set("X-Goog-FieldMask", getFieldMask)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Place{}, fmt.Errorf("maps: get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Place{}, ErrNotFound
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Place{}, fmt.Errorf("maps: read get response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Place{}, fmt.Errorf("maps: get: http %d: %s", resp.StatusCode, string(raw))
	}

	var gp googlePlace
	if err := json.Unmarshal(raw, &gp); err != nil {
		return Place{}, fmt.Errorf("maps: decode get response: %w", err)
	}
	return gp.toPlace(), nil
}
