package listing

import (
	"reflect"
	"testing"
)

func TestNormalizeFeature(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// Hot tub aliases all collapse.
		{"hot tub", "Hot Tub"},
		{"Jacuzzi", "Hot Tub"},
		{"Private jetted tub", "Hot Tub"},
		{"Spa", "Hot Tub"},
		// "space" must NOT trigger the spa rule.
		{"Workspace", "Workspace"},

		{"Swimming pool", "Pool"},
		{"beachfront views", "Waterfront"},
		{"Ocean view", "Waterfront"},
		{"Wi-Fi 6", "WiFi"},
		{"Wireless internet", "WiFi"},
		{"A/C", "Air Conditioning"},
		{"Heated pool", "Pool"}, // pool wins over heating when both words appear
		{"Heater", "Heating"},
		{"BBQ grill area", "BBQ Grill"},
		{"Fire pit", "Fireplace"},
		{"Private garden", "Outdoor Space"},
		{"Elevator access", "Elevator"},

		// Badges preserved verbatim.
		{"Superhost", "Superhost"},
		{"Guest favorite", "Guest favorite"},

		// Unknown falls through to title-case.
		{"whatever else", "Whatever Else"},

		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := NormalizeFeature(c.in)
			if got != c.want {
				t.Errorf("NormalizeFeature(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

func TestNormalizeFeaturesDedupesPreservingOrder(t *testing.T) {
	in := []string{"Jacuzzi", "WiFi", "hot tub", "Pool", "Swimming Pool", "", "Wireless"}
	want := []string{"Hot Tub", "WiFi", "Pool"}
	got := NormalizeFeatures(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeFeatures() = %v; want %v", got, want)
	}
}
