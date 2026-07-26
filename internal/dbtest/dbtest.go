// Package dbtest provides a throwaway MongoDB for integration tests.
//
// Tests that need a real database call New(t). When MONGODB_TEST_URI is unset
// the test skips, so `go test ./...` still works on a machine with no Mongo.
//
// # Safety
//
// Every test gets its own uniquely-named database which is dropped on
// cleanup. The name is generated here and always carries testDBPrefix; New
// refuses to run against anything that doesn't, so a stray MONGODB_TEST_URI
// with a database path (e.g. ".../real_estayer") cannot cause a test to
// create, mutate, or drop collections in a real deployment.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/database"
)

// testDBPrefix marks databases this package owns. Anything without it is
// treated as production and refused.
const testDBPrefix = "estayer_test_"

// URIEnv is the environment variable holding the test server's connection
// string, e.g. "mongodb://127.0.0.1:27017".
const URIEnv = "MONGODB_TEST_URI"

// New returns a DB backed by a fresh, empty, uniquely-named database and
// registers cleanup that drops it. Skips the test if URIEnv is unset.
func New(t *testing.T) *database.DB {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv(URIEnv))
	if uri == "" {
		t.Skipf("%s not set; skipping integration test", URIEnv)
	}

	// Strip any database path from the URI. We always choose the name
	// ourselves so the guard below is meaningful.
	uri = stripDatabasePath(uri)

	name := testDBPrefix + randomSuffix(t)
	if !strings.HasPrefix(name, testDBPrefix) {
		t.Fatalf("refusing to run against non-test database %q", name)
	}

	db, err := database.Connect(uri, name)
	if err != nil {
		t.Fatalf("connect to test mongo at %s: %v", uri, err)
	}
	if got := db.Database.Name(); !strings.HasPrefix(got, testDBPrefix) {
		// Belt and braces: if Connect ever resolves a different name than we
		// asked for, stop before touching it.
		_ = db.Disconnect(context.Background())
		t.Fatalf("refusing to run against database %q (expected %s* prefix)", got, testDBPrefix)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := db.Database.Drop(ctx); err != nil {
			t.Logf("warning: could not drop test database %s: %v", name, err)
		}
		if err := db.Disconnect(ctx); err != nil {
			t.Logf("warning: disconnect failed: %v", err)
		}
	})

	return db
}

// Context returns a context with a deadline suitable for a single test.
func Context(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// stripDatabasePath removes the database component of a Mongo URI, keeping
// any query string. A URI we cannot parse is returned unchanged; Connect will
// fail on it and the test reports that.
func stripDatabasePath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	u.Path = ""
	return u.String()
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate test database name: %v", err)
	}
	return fmt.Sprintf("%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
}
