package migrations

import (
	"context"
	"errors"
	"testing"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/dbtest"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// These run against a real MongoDB because that is the only way to verify a
// migration: the whole point of the code under test is what it does to a
// database. Set MONGODB_TEST_URI to enable; otherwise they skip.

func appliedVersions(t *testing.T, ctx context.Context, db *database.DB) map[int]string {
	t.Helper()
	cur, err := db.Collection("schema_migrations").Find(ctx, bson.M{})
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	out := map[int]string{}
	for cur.Next(ctx) {
		var rec migrationRecord
		if err := cur.Decode(&rec); err != nil {
			t.Fatalf("decode migration record: %v", err)
		}
		out[rec.Version] = rec.Name
	}
	return out
}

func TestRunAppliesAllRegisteredMigrations(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	applied := appliedVersions(t, ctx, db)
	if len(applied) != len(registered) {
		t.Fatalf("recorded %d migrations, want %d registered", len(applied), len(registered))
	}
	for _, m := range registered {
		if got, ok := applied[m.Version]; !ok {
			t.Errorf("migration %d (%s) was not recorded", m.Version, m.Name)
		} else if got != m.Name {
			t.Errorf("migration %d recorded as %q, want %q", m.Version, got, m.Name)
		}
	}
}

// The runner promises idempotency: a second Run must not re-apply anything.
func TestRunIsIdempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	first := appliedVersions(t, ctx, db)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	second := appliedVersions(t, ctx, db)

	if len(first) != len(second) {
		t.Errorf("record count changed across runs: %d -> %d", len(first), len(second))
	}
	for v, name := range first {
		if second[v] != name {
			t.Errorf("version %d changed from %q to %q", v, name, second[v])
		}
	}
}

// A migration already recorded must not run again — proven by giving it a
// side effect that would be visible if it did.
func TestRunSkipsAlreadyAppliedVersions(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Insert a doc m001 would rewrite, then run again. Since m001 is already
	// recorded, the doc must be left alone.
	_, err := db.Collection("listings").InsertOne(ctx, bson.M{
		"features": []string{"  WiFi  "},
	})
	if err != nil {
		t.Fatalf("seed listing: %v", err)
	}

	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	var doc struct {
		Features []string `bson:"features"`
	}
	if err := db.Collection("listings").FindOne(ctx, bson.M{}).Decode(&doc); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(doc.Features) != 1 || doc.Features[0] != "  WiFi  " {
		t.Errorf("features = %q; an already-applied migration re-ran", doc.Features)
	}
}

// --- m001: normalize_listing_features ---

func TestM001NormalizesFeatures(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	res, err := db.Collection("listings").InsertOne(ctx, bson.M{
		"features": []string{"wifi", "WIFI", "  Pool  "},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := normalizeListingFeatures(ctx, db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var doc struct {
		Features []string `bson:"features"`
	}
	if err := db.Collection("listings").FindOne(ctx, bson.M{"_id": res.InsertedID}).Decode(&doc); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Exact output belongs to listing.NormalizeFeatures (tested separately);
	// here we assert the migration actually wrote a changed, deduped value.
	if len(doc.Features) >= 3 {
		t.Errorf("features = %q, expected normalisation to dedupe", doc.Features)
	}
	for _, f := range doc.Features {
		if f != trimmed(f) {
			t.Errorf("feature %q still has surrounding whitespace", f)
		}
	}
}

func trimmed(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func TestM001LeavesAlreadyNormalisedDocsUntouched(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	// Run once to reach a normalised state, capture it, then run again.
	if _, err := db.Collection("listings").InsertOne(ctx, bson.M{
		"features": []string{"wifi", "WIFI", "  Pool  "},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := normalizeListingFeatures(ctx, db); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	var before struct {
		Features []string `bson:"features"`
	}
	if err := db.Collection("listings").FindOne(ctx, bson.M{}).Decode(&before); err != nil {
		t.Fatalf("read: %v", err)
	}

	if err := normalizeListingFeatures(ctx, db); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	var after struct {
		Features []string `bson:"features"`
	}
	if err := db.Collection("listings").FindOne(ctx, bson.M{}).Decode(&after); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(before.Features) != len(after.Features) {
		t.Fatalf("second pass changed the doc: %q -> %q", before.Features, after.Features)
	}
	for i := range before.Features {
		if before.Features[i] != after.Features[i] {
			t.Errorf("second pass changed feature %d: %q -> %q", i, before.Features[i], after.Features[i])
		}
	}
}

// Docs with no features must not be touched or acquire the field.
func TestM001IgnoresDocsWithoutFeatures(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if _, err := db.Collection("listings").InsertOne(ctx, bson.M{"title": "no features here"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := normalizeListingFeatures(ctx, db); err != nil {
		t.Fatalf("migration: %v", err)
	}
	var doc bson.M
	if err := db.Collection("listings").FindOne(ctx, bson.M{}).Decode(&doc); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := doc["features"]; ok {
		t.Error("migration added a features field to a doc that had none")
	}
}

func TestM001OnEmptyCollection(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	if err := normalizeListingFeatures(ctx, db); err != nil {
		t.Errorf("empty collection should not error: %v", err)
	}
}

// --- m002: audit_logs TTL ---

func TestM002CreatesTTLIndex(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if err := auditLogsTTL(ctx, db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	cur, err := db.Collection("audit_logs").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer func() { _ = cur.Close(ctx) }()

	var found bool
	for cur.Next(ctx) {
		var idx bson.M
		if err := cur.Decode(&idx); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		if idx["name"] != "created_at_ttl" {
			continue
		}
		found = true
		// 365 days, the documented retention window.
		got, ok := idx["expireAfterSeconds"]
		if !ok {
			t.Fatal("created_at_ttl index has no expireAfterSeconds")
		}
		if toInt64(got) != 365*24*60*60 {
			t.Errorf("expireAfterSeconds = %v, want %d", got, 365*24*60*60)
		}
	}
	if !found {
		t.Error("created_at_ttl index was not created")
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return -1
}

func TestM002IsIdempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	if err := auditLogsTTL(ctx, db); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := auditLogsTTL(ctx, db); err != nil {
		t.Errorf("re-applying an identical TTL index must be a no-op, got: %v", err)
	}
}

// The migration's doc comment promises it fails loudly rather than silently
// changing a TTL — that guarantee is what stops a retention change from
// quietly deleting data, so it is worth pinning.
func TestM002FailsOnConflictingExistingTTL(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	_, err := db.Collection("audit_logs").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(60).SetName("created_at_ttl"),
	})
	if err != nil {
		t.Fatalf("seed conflicting index: %v", err)
	}

	if err := auditLogsTTL(ctx, db); err == nil {
		t.Error("expected a conflicting TTL to fail the migration, got nil")
	} else if !mongo.IsDuplicateKeyError(err) && !errors.As(err, new(mongo.CommandError)) {
		t.Logf("failed as expected with: %v", err)
	}
}

// --- m003: share_slug normalisation ---

func TestM003UnsetsEmptyShareSlugs(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	docs := []any{
		bson.M{"name": "empty slug", "share_slug": ""},
		bson.M{"name": "real slug", "share_slug": "abc123"},
		bson.M{"name": "absent slug"},
	}
	if _, err := db.Collection("trips").InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := shareSlugNormalize(ctx, db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// The empty one must have the field removed, not set to null.
	var empty bson.M
	if err := db.Collection("trips").FindOne(ctx, bson.M{"name": "empty slug"}).Decode(&empty); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, present := empty["share_slug"]; present {
		t.Errorf("share_slug still present as %v; must be unset for the sparse index", empty["share_slug"])
	}

	// A real slug must survive untouched.
	var real bson.M
	if err := db.Collection("trips").FindOne(ctx, bson.M{"name": "real slug"}).Decode(&real); err != nil {
		t.Fatalf("read: %v", err)
	}
	if real["share_slug"] != "abc123" {
		t.Errorf("share_slug = %v, want abc123 preserved", real["share_slug"])
	}

	// A doc that never had one must not gain it.
	var absent bson.M
	if err := db.Collection("trips").FindOne(ctx, bson.M{"name": "absent slug"}).Decode(&absent); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, present := absent["share_slug"]; present {
		t.Error("migration added share_slug to a doc that had none")
	}
}

func TestM003IsIdempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)

	if _, err := db.Collection("trips").InsertOne(ctx, bson.M{"share_slug": ""}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := shareSlugNormalize(ctx, db); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := shareSlugNormalize(ctx, db); err != nil {
		t.Errorf("second pass errored: %v", err)
	}
	n, err := db.Collection("trips").CountDocuments(ctx, bson.M{"share_slug": bson.M{"$exists": true}})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d docs still carry share_slug", n)
	}
}

func TestM003OnEmptyCollection(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	if err := shareSlugNormalize(ctx, db); err != nil {
		t.Errorf("empty collection should not error: %v", err)
	}
}

// --- Register invariants (no database needed) ---

func TestRegisterRejectsNonPositiveVersion(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for version 0")
		}
	}()
	Register(Migration{Version: 0, Name: "bad"})
}

func TestRegisterRejectsDuplicateVersion(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a duplicate version")
		}
	}()
	// Version 1 is already registered by m001's init.
	Register(Migration{Version: 1, Name: "collides"})
}

func TestRegisteredVersionsAreUniqueAndPositive(t *testing.T) {
	seen := map[int]bool{}
	for _, m := range registered {
		if m.Version <= 0 {
			t.Errorf("migration %q has non-positive version %d", m.Name, m.Version)
		}
		if seen[m.Version] {
			t.Errorf("duplicate migration version %d", m.Version)
		}
		seen[m.Version] = true
		if m.Apply == nil {
			t.Errorf("migration %d (%s) has a nil Apply", m.Version, m.Name)
		}
	}
}
