package migrations

import (
	"context"
	"log/slog"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/listing"
	"go.mongodb.org/mongo-driver/bson"
)

func init() {
	Register(Migration{
		Version: 4,
		Name:    "listing_place_backfill",
		Apply:   listingPlaceBackfill,
	})
}

// listingPlaceBackfill derives region/country from each listing's location
// text.
//
// The scraper only sets those fields from the listing page's JSON-LD or
// og:title, and the fast path's HTTP enrichment usually gets neither, so 280
// of 366 documents had no region and no country at all. GetStates() filters on
// `country IN (United States, USA, ...)`, so the /listings State filter could
// only see the 86 older documents — every one of them Michigan — and reported
// a single state for a database spanning Indiana, Maryland, Texas and Ontario.
//
// Writes only fields that are currently absent or empty, and only when they
// can be derived: a bare neighbourhood ("East Austin") stays unset rather than
// being guessed into the wrong state. Existing values are also canonicalised,
// so legacy "USA"/"michigan" become "United States"/"Michigan" and stop
// depending on the repository's spelling-variant list.
func listingPlaceBackfill(ctx context.Context, db *database.DB) error {
	coll := db.Collection("listings")

	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return err
	}
	defer func() { _ = cur.Close(ctx) }()

	var scanned, updated, unresolved int
	for cur.Next(ctx) {
		var doc struct {
			ID       any    `bson:"_id"`
			Location string `bson:"location"`
			Region   string `bson:"region"`
			Country  string `bson:"country"`
		}
		if err := cur.Decode(&doc); err != nil {
			return err
		}
		scanned++

		region, country := doc.Region, doc.Country

		// Canonicalise what is already there before deriving anything.
		if c := listing.NormalizeCountry(country); c != "" {
			country = c
		}
		if r := listing.NormalizeRegion(region); r != "" {
			region = r
		}

		if region == "" || country == "" {
			_, parsedRegion, parsedCountry := listing.ParsePlace(doc.Location)
			if region == "" {
				region = parsedRegion
			}
			if country == "" {
				country = parsedCountry
			}
		}

		set := bson.M{}
		if region != doc.Region && region != "" {
			set["region"] = region
		}
		if country != doc.Country && country != "" {
			set["country"] = country
		}
		if len(set) == 0 {
			if doc.Region == "" && doc.Country == "" {
				unresolved++
			}
			continue
		}
		if _, err := coll.UpdateByID(ctx, doc.ID, bson.M{"$set": set}); err != nil {
			return err
		}
		updated++
	}
	if err := cur.Err(); err != nil {
		return err
	}

	slog.Info("listing_place_backfill",
		"scanned", scanned, "updated", updated,
		// Neighbourhood-only locations that genuinely cannot be resolved.
		// Expected to be non-zero; they are excluded from the filters rather
		// than filed under a guess.
		"unresolved", unresolved)
	return nil
}
