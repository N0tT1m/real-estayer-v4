package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// databaseFromURI is pure, so it is tested without a server.
func TestDatabaseFromURI(t *testing.T) {
	tests := map[string]string{
		"mongodb://localhost:27017/real_estayer":       "real_estayer",
		"mongodb://localhost:27017/other_db":           "other_db",
		"mongodb://localhost:27017":                    "",
		"mongodb://localhost:27017/":                   "",
		"mongodb://user:pass@host:27017/mydb":          "mydb",
		"mongodb+srv://user:pass@cluster.example/mydb": "mydb",
		"mongodb://host:27017/mydb?retryWrites=true":   "mydb",
		"":             "",
		"://not a uri": "",
	}
	for uri, want := range tests {
		if got := databaseFromURI(uri); got != want {
			t.Errorf("databaseFromURI(%q) = %q, want %q", uri, got, want)
		}
	}
}

// Name resolution is the part that regressed before: the database was
// hardcoded, so MONGODB_DATABASE was ignored. Pure, so no server needed —
// which also means no test has to connect to the default (production)
// database just to observe the fallback.
func TestResolveDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		uri     string
		dbName  string
		want    string
		comment string
	}{
		{"explicit name wins over URI path", "mongodb://h:27017/from_uri", "explicit", "explicit",
			"an explicit dbName must beat the URI path"},
		{"URI path used when no explicit name", "mongodb://h:27017/from_uri", "", "from_uri",
			"with no explicit name the URI path is used"},
		{"default when neither supplied", "mongodb://h:27017", "", defaultDatabase,
			"preserves the historical hardcoded value"},
		{"default when URI has bare slash", "mongodb://h:27017/", "", defaultDatabase,
			"an empty path is not a database name"},
		{"explicit name wins over nothing", "mongodb://h:27017", "explicit", "explicit",
			"explicit name applies even with no URI path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveDatabaseName(tc.uri, tc.dbName); got != tc.want {
				t.Errorf("resolveDatabaseName(%q, %q) = %q, want %q (%s)",
					tc.uri, tc.dbName, got, tc.want, tc.comment)
			}
		})
	}
}

// Connect must actually apply that resolution. Only test-prefixed names are
// used here so this can never open the production database.
func TestConnectAppliesResolvedName(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping integration test")
	}
	for _, tc := range []struct{ name, uri, dbName, want string }{
		{"explicit", uri + "/estayer_test_from_uri", "estayer_test_explicit", "estayer_test_explicit"},
		{"from URI path", uri + "/estayer_test_from_uri", "", "estayer_test_from_uri"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Connect(tc.uri, tc.dbName)
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if strings.HasPrefix(db.Database.Name(), "estayer_test_") {
					_ = db.Database.Drop(ctx)
				}
				_ = db.Disconnect(ctx)
			})
			if got := db.Database.Name(); got != tc.want {
				t.Errorf("database = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConnectFailsOnUnreachableServer(t *testing.T) {
	// Port 1 is reserved and never a Mongo server; server selection should
	// give up rather than hang, because Connect sets a selection timeout.
	start := time.Now()
	_, err := Connect("mongodb://127.0.0.1:1/estayer_test_unreachable", "")
	if err == nil {
		t.Fatal("expected an error connecting to an unreachable server")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("Connect took %v; server selection timeout is not bounding it", elapsed)
	}
}

func TestCollectionReturnsNamedCollection(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping integration test")
	}
	db, err := Connect(uri, "estayer_test_collection_helper")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = db.Database.Drop(ctx)
		_ = db.Disconnect(ctx)
	})
	if got := db.Collection("listings").Name(); got != "listings" {
		t.Errorf("Collection(listings).Name() = %q", got)
	}
}

// createIndexes runs on every Connect and swallows individual failures by
// design. This checks it actually installs the indexes the app relies on.
func TestConnectCreatesCoreIndexes(t *testing.T) {
	uri := strings.TrimSpace(os.Getenv("MONGODB_TEST_URI"))
	if uri == "" {
		t.Skip("MONGODB_TEST_URI not set; skipping integration test")
	}
	db, err := Connect(uri, "estayer_test_indexes")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Cleanup needs its own context: the deferred cancel above fires when the
	// test returns, which is *before* t.Cleanup runs, so reusing ctx here
	// would silently skip the drop and leak the database.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := db.Database.Drop(cleanupCtx); err != nil {
			t.Logf("warning: could not drop test database: %v", err)
		}
		_ = db.Disconnect(cleanupCtx)
	})

	// A representative sample: uniqueness constraints the app depends on for
	// correctness, not merely for speed.
	checks := []struct{ coll, key string }{
		{"users", "email"},
		{"sessions", "token"},
		{"listings", "url"},
	}
	for _, c := range checks {
		cur, err := db.Collection(c.coll).Indexes().List(ctx)
		if err != nil {
			t.Fatalf("list %s indexes: %v", c.coll, err)
		}
		var found bool
		for cur.Next(ctx) {
			var idx struct {
				Key    map[string]any `bson:"key"`
				Unique bool           `bson:"unique"`
			}
			if err := cur.Decode(&idx); err != nil {
				t.Fatalf("decode index: %v", err)
			}
			if _, ok := idx.Key[c.key]; ok && idx.Unique {
				found = true
			}
		}
		_ = cur.Close(ctx)
		if !found {
			t.Errorf("%s.%s has no unique index", c.coll, c.key)
		}
	}
}
