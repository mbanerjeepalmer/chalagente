package maps

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- MockClient ---------------------------------------------------------

func TestMockClient_Search_CaseInsensitiveSubstring(t *testing.T) {
	c := DefaultMockClient()
	ctx := context.Background()

	// Substring of a known name, lowercased.
	got, err := c.Search(ctx, "viajes")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one match for 'viajes', got 0")
	}
	var found bool
	for _, p := range got {
		if strings.Contains(strings.ToLower(p.Name), "viajes") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a place with 'viajes' in its name, got %+v", got)
	}

	// Match by category.
	got, err = c.Search(ctx, "restaurant")
	if err != nil {
		t.Fatalf("Search categories: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one restaurant match")
	}

	// Match by address fragment.
	got, err = c.Search(ctx, "ciudad de méxico")
	if err != nil {
		t.Fatalf("Search address: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected at least one address match for 'ciudad de méxico'")
	}
}

func TestMockClient_Search_NoMatchesEmptyNoError(t *testing.T) {
	c := DefaultMockClient()
	got, err := c.Search(context.Background(), "zzzz-nothing-matches-zzzz")
	if err != nil {
		t.Fatalf("expected nil error for no matches, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

func TestMockClient_Get_NotFound(t *testing.T) {
	c := DefaultMockClient()
	_, err := c.Get(context.Background(), "no-such-place-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMockClient_Get_Known(t *testing.T) {
	c := DefaultMockClient()
	if len(MockPlaces) == 0 {
		t.Fatalf("expected canned MockPlaces to be non-empty")
	}
	want := MockPlaces[0]
	got, err := c.Get(context.Background(), want.PlaceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PlaceID != want.PlaceID {
		t.Fatalf("PlaceID mismatch: %s vs %s", got.PlaceID, want.PlaceID)
	}
	if got.Name != want.Name {
		t.Fatalf("Name mismatch: %s vs %s", got.Name, want.Name)
	}
}

// --- GoogleClient -------------------------------------------------------

func TestGoogleClient_NoAPIKey(t *testing.T) {
	c := &GoogleClient{}
	if _, err := c.Search(context.Background(), "anything"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("Search: expected ErrNoAPIKey, got %v", err)
	}
	if _, err := c.Get(context.Background(), "anyid"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("Get: expected ErrNoAPIKey, got %v", err)
	}
}

func TestGoogleClient_Search_HTTPShape(t *testing.T) {
	var (
		gotMethod    string
		gotPath      string
		gotAPIKey    string
		gotFieldMask string
		gotBody      []byte
	)

	canned := `{
        "places": [
            {
                "id": "ChIJplace1",
                "displayName": {"text": "Tacos El Güero", "languageCode": "es"},
                "formattedAddress": "Av. Reforma 123, Ciudad de México",
                "internationalPhoneNumber": "+52 55 1234 5678",
                "websiteUri": "https://tacoselguero.example",
                "regularOpeningHours": {
                    "weekdayDescriptions": [
                        "lunes: 9:00 – 22:00",
                        "martes: 9:00 – 22:00"
                    ]
                },
                "types": ["restaurant", "mexican_restaurant"],
                "location": {"latitude": 19.4326, "longitude": -99.1332}
            }
        ]
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-Goog-Api-Key")
		gotFieldMask = r.Header.Get("X-Goog-FieldMask")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(canned))
	}))
	defer srv.Close()

	c := &GoogleClient{
		APIKey:     "test-key-123",
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	}

	places, err := c.Search(context.Background(), "tacos cdmx")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/v1/places:searchText" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAPIKey != "test-key-123" {
		t.Fatalf("unexpected X-Goog-Api-Key: %q", gotAPIKey)
	}
	if gotFieldMask == "" {
		t.Fatalf("expected X-Goog-FieldMask header to be set")
	}
	// Check the field mask asks for the fields we care about.
	for _, want := range []string{"places.id", "places.displayName", "places.formattedAddress"} {
		if !strings.Contains(gotFieldMask, want) {
			t.Fatalf("field mask missing %q: %s", want, gotFieldMask)
		}
	}

	var reqBody map[string]any
	if err := json.Unmarshal(gotBody, &reqBody); err != nil {
		t.Fatalf("request body not JSON: %v (raw=%s)", err, string(gotBody))
	}
	if reqBody["textQuery"] != "tacos cdmx" {
		t.Fatalf("expected textQuery=tacos cdmx, got %v", reqBody["textQuery"])
	}

	if len(places) != 1 {
		t.Fatalf("expected 1 place, got %d", len(places))
	}
	p := places[0]
	if p.PlaceID != "ChIJplace1" {
		t.Fatalf("PlaceID: %s", p.PlaceID)
	}
	if p.Name != "Tacos El Güero" {
		t.Fatalf("Name: %s", p.Name)
	}
	if p.Address != "Av. Reforma 123, Ciudad de México" {
		t.Fatalf("Address: %s", p.Address)
	}
	if p.Phone != "+52 55 1234 5678" {
		t.Fatalf("Phone: %s", p.Phone)
	}
	if p.Website != "https://tacoselguero.example" {
		t.Fatalf("Website: %s", p.Website)
	}
	if !strings.Contains(p.Hours, "lunes") || !strings.Contains(p.Hours, "martes") {
		t.Fatalf("Hours not pre-formatted as expected: %q", p.Hours)
	}
	if len(p.Categories) != 2 || p.Categories[0] != "restaurant" {
		t.Fatalf("Categories: %+v", p.Categories)
	}
	if p.Lat != 19.4326 || p.Lng != -99.1332 {
		t.Fatalf("Lat/Lng: %v, %v", p.Lat, p.Lng)
	}
}

func TestGoogleClient_Get_HTTPShape(t *testing.T) {
	var (
		gotMethod    string
		gotPath      string
		gotAPIKey    string
		gotFieldMask string
	)

	canned := `{
        "id": "ChIJgetplace",
        "displayName": {"text": "Salón Bella", "languageCode": "es"},
        "formattedAddress": "Calle 5 de Mayo 42, Oaxaca",
        "internationalPhoneNumber": "+52 951 555 0000",
        "websiteUri": "https://salonbella.example",
        "regularOpeningHours": {
            "weekdayDescriptions": ["lunes: 10:00 – 19:00"]
        },
        "types": ["hair_salon", "beauty_salon"],
        "location": {"latitude": 17.0732, "longitude": -96.7266}
    }`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("X-Goog-Api-Key")
		gotFieldMask = r.Header.Get("X-Goog-FieldMask")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(canned))
	}))
	defer srv.Close()

	c := &GoogleClient{
		APIKey:     "test-key-456",
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	}

	p, err := c.Get(context.Background(), "ChIJgetplace")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/v1/places/ChIJgetplace" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAPIKey != "test-key-456" {
		t.Fatalf("unexpected X-Goog-Api-Key: %q", gotAPIKey)
	}
	if gotFieldMask == "" {
		t.Fatalf("expected X-Goog-FieldMask header to be set")
	}

	if p.PlaceID != "ChIJgetplace" {
		t.Fatalf("PlaceID: %s", p.PlaceID)
	}
	if p.Name != "Salón Bella" {
		t.Fatalf("Name: %s", p.Name)
	}
	if p.Address != "Calle 5 de Mayo 42, Oaxaca" {
		t.Fatalf("Address: %s", p.Address)
	}
	if p.Phone != "+52 951 555 0000" {
		t.Fatalf("Phone: %s", p.Phone)
	}
	if !strings.Contains(p.Hours, "lunes") {
		t.Fatalf("Hours: %q", p.Hours)
	}
	if len(p.Categories) != 2 || p.Categories[1] != "beauty_salon" {
		t.Fatalf("Categories: %+v", p.Categories)
	}
	if p.Lat != 17.0732 || p.Lng != -96.7266 {
		t.Fatalf("Lat/Lng: %v, %v", p.Lat, p.Lng)
	}
}

func TestGoogleClient_Get_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"message":"not found"}}`))
	}))
	defer srv.Close()

	c := &GoogleClient{
		APIKey:     "k",
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	}
	if _, err := c.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Make sure both client types implement the Client interface at compile time.
var _ Client = (*MockClient)(nil)
var _ Client = (*GoogleClient)(nil)
