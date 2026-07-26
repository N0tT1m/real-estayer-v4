package listing

import (
	"regexp"
	"strconv"
	"strings"
)

// Facts are the structured properties a guest actually filters on. Airbnb
// ships them as prose ("this 2-bedroom, 2-bath condo") and as stray entries in
// the amenity list ("2 bedrooms"), so they have to be recovered from both.
//
// A zero value means "unknown", never "none" — callers must not render 0
// bedrooms as a fact. Extraction is best-effort by nature.
type Facts struct {
	Bedrooms  int     `bson:"bedrooms,omitempty"  json:"bedrooms,omitempty"`
	Bathrooms float64 `bson:"bathrooms,omitempty" json:"bathrooms,omitempty"` // .5 for half baths
	Beds      int     `bson:"beds,omitempty"      json:"beds,omitempty"`
	Sleeps    int     `bson:"sleeps,omitempty"    json:"sleeps,omitempty"`
}

// Complete reports whether every field was recovered. Used to decide whether
// an LLM pass is worth spending on a listing.
func (f Facts) Complete() bool {
	return f.Bedrooms > 0 && f.Bathrooms > 0 && f.Beds > 0 && f.Sleeps > 0
}

// Empty reports whether nothing at all was recovered.
func (f Facts) Empty() bool {
	return f.Bedrooms == 0 && f.Bathrooms == 0 && f.Beds == 0 && f.Sleeps == 0
}

// merge fills only the gaps in f from other, so a cheaper, more trustworthy
// source is never overwritten by a later, looser one.
func (f Facts) merge(other Facts) Facts {
	if f.Bedrooms == 0 {
		f.Bedrooms = other.Bedrooms
	}
	if f.Bathrooms == 0 {
		f.Bathrooms = other.Bathrooms
	}
	if f.Beds == 0 {
		f.Beds = other.Beds
	}
	if f.Sleeps == 0 {
		f.Sleeps = other.Sleeps
	}
	return f
}

var (
	// "2 bedrooms", "1 bedroom", "studio"
	reBedrooms = regexp.MustCompile(`(?i)\b(\d+)\s*-?\s*bedrooms?\b`)
	// "2 baths", "1.5 bathrooms", "2 ba"
	reBathrooms = regexp.MustCompile(`(?i)\b(\d+(?:\.\d)?)\s*-?\s*(?:bath(?:room)?s?|ba)\b`)
	// "3 beds", "1 double bed", "2 queen beds"
	reBeds = regexp.MustCompile(`(?i)\b(\d+)\s+(?:\w+\s+){0,2}beds?\b`)
	// "sleeps 6", "accommodates 8", "up to 4 guests"
	reSleeps = regexp.MustCompile(`(?i)\b(?:sleeps|accommodates)\s+(\d+)\b|\bup to (\d+) guests?\b|\b(\d+)\s+guests?\b`)
	// "half bath", "1/2 bath"
	reHalfBath = regexp.MustCompile(`(?i)\b(?:half|1/2)\s*-?\s*bath`)
	reStudio   = regexp.MustCompile(`(?i)\bstudio\b`)
)

// FactsFromText recovers what it can from free prose — a listing description
// or title. Deterministic and cheap; run this before spending an LLM call.
func FactsFromText(text string) Facts {
	var f Facts
	if text == "" {
		return f
	}

	if m := reBedrooms.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 50 {
			f.Bedrooms = n
		}
	} else if reStudio.MatchString(text) {
		// A studio has no separate bedroom. Record it as 0 sleeping rooms but
		// still a known quantity by setting beds if we find them below.
		f.Bedrooms = 0
	}

	if m := reBathrooms.FindStringSubmatch(text); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 && v < 50 {
			f.Bathrooms = v
		}
	} else if reHalfBath.MatchString(text) {
		f.Bathrooms = 0.5
	}

	if m := reBeds.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 && n < 100 {
			f.Beds = n
		}
	}

	if m := reSleeps.FindStringSubmatch(text); m != nil {
		for _, g := range m[1:] {
			if g == "" {
				continue
			}
			if n, err := strconv.Atoi(g); err == nil && n > 0 && n < 100 {
				f.Sleeps = n
				break
			}
		}
	}
	return f
}

// FactsFromFeatures pulls counts out of the scraped amenity list, where Airbnb
// mixes them in among real amenities ("2 bedrooms", "1 double bed"). These
// entries are more reliable than prose because they are already structured,
// so callers should prefer them.
func FactsFromFeatures(features []string) Facts {
	var f Facts
	for _, raw := range features {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		got := FactsFromText(s)
		// A bare "1 bed" entry must not be read as "sleeps 1".
		got.Sleeps = 0
		f = f.merge(got)
	}
	return f
}

// ExtractFacts combines both deterministic sources, preferring the structured
// amenity entries over prose.
func ExtractFacts(features []string, description, title string) Facts {
	f := FactsFromFeatures(features)
	f = f.merge(FactsFromText(description))
	f = f.merge(FactsFromText(title))
	return f
}
