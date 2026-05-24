package maps

import (
	"context"
	"strings"
)

// MockPlaces is the canned dataset used by MockClient. Exposed as a
// package-level var so tests and dev wiring can reach for the same data.
var MockPlaces = []Place{
	{
		PlaceID: "mock-viajes-mexico-tuyo",
		Name:    "Viajes México Tuyo",
		Address: "Av. Insurgentes Sur 1602, Ciudad de México, CDMX 03940",
		Phone:   "+52 55 5512 3456",
		Website: "https://viajesmexicotuyo.example",
		Hours: "Lun-Vie 9:00-18:00\n" +
			"Sáb 10:00-14:00\n" +
			"Dom cerrado",
		Categories: []string{"travel_agency", "tour_operator"},
		Lat:        19.3656,
		Lng:        -99.1773,
	},
	{
		PlaceID: "mock-tacos-el-guero",
		Name:    "Tacos El Güero",
		Address: "Calle 5 de Mayo 87, Centro, Ciudad de México, CDMX 06000",
		Phone:   "+52 55 5510 9988",
		Website: "https://tacoselguero.example",
		Hours: "Lun-Dom 11:00-23:00",
		Categories: []string{"restaurant", "mexican_restaurant", "taco_restaurant"},
		Lat:        19.4357,
		Lng:        -99.1390,
	},
	{
		PlaceID: "mock-salon-bella-oaxaca",
		Name:    "Salón Bella Oaxaca",
		Address: "Calle García Vigil 204, Centro, Oaxaca de Juárez, Oaxaca 68000",
		Phone:   "+52 951 514 7700",
		Website: "https://salonbella.example",
		Hours: "Mar-Sáb 10:00-19:00\n" +
			"Dom-Lun cerrado",
		Categories: []string{"hair_salon", "beauty_salon"},
		Lat:        17.0732,
		Lng:        -96.7266,
	},
	{
		PlaceID: "mock-cafe-cancun",
		Name:    "Café Maya Cancún",
		Address: "Av. Tulum 200, Centro, Cancún, Quintana Roo 77500",
		Phone:   "+52 998 884 0011",
		Website: "https://cafemaya.example",
		Hours: "Lun-Dom 7:00-22:00",
		Categories: []string{"cafe", "coffee_shop", "breakfast_restaurant"},
		Lat:        21.1619,
		Lng:        -86.8515,
	},
}

// MockClient is an in-memory Client backed by a canned slice of places.
// Search does case-insensitive substring matching across name, categories,
// and address. Get looks up by PlaceID.
type MockClient struct {
	Places []Place
}

// DefaultMockClient returns a MockClient seeded with MockPlaces.
func DefaultMockClient() *MockClient {
	// Copy so callers can mutate without poisoning the package-level slice.
	places := make([]Place, len(MockPlaces))
	copy(places, MockPlaces)
	return &MockClient{Places: places}
}

// Search returns places whose name, any category, or address contains query
// (case-insensitive). An empty result is not an error.
func (m *MockClient) Search(_ context.Context, query string) ([]Place, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return []Place{}, nil
	}
	var out []Place
	for _, p := range m.Places {
		if matchesQuery(p, q) {
			out = append(out, p)
		}
	}
	if out == nil {
		return []Place{}, nil
	}
	return out, nil
}

// Get returns the place with the given id, or ErrNotFound.
func (m *MockClient) Get(_ context.Context, placeID string) (Place, error) {
	for _, p := range m.Places {
		if p.PlaceID == placeID {
			return p, nil
		}
	}
	return Place{}, ErrNotFound
}

func matchesQuery(p Place, q string) bool {
	if strings.Contains(strings.ToLower(p.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(p.Address), q) {
		return true
	}
	for _, cat := range p.Categories {
		if strings.Contains(strings.ToLower(cat), q) {
			return true
		}
	}
	return false
}
