package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/dbtest"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Integration tests against a real MongoDB. The unit tests in
// listing_repo_test.go cover filter construction; these cover what those
// filters actually do to a server, which is the half that can't be faked.
// Set MONGODB_TEST_URI to enable; otherwise they skip.

// --- SessionRepository ---
//
// Session handling is security-critical: rotation on login and revocation on
// password change both depend on these queries behaving exactly right.

func TestSessionCreateAndFindByToken(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	userID := primitive.NewObjectID()
	s := &models.Session{UserID: userID, Token: "tok-1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.ID.IsZero() {
		t.Error("Create did not assign an ID")
	}

	got, err := repo.FindByToken(ctx, "tok-1")
	if err != nil {
		t.Fatalf("FindByToken: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID = %v, want %v", got.UserID, userID)
	}
}

func TestSessionFindByTokenUnknown(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	_, err := repo.FindByToken(ctx, "nope")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

// An expired session must be rejected AND cleaned up, not silently returned.
func TestSessionFindByTokenExpiredIsRejectedAndDeleted(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	s := &models.Session{
		UserID:    primitive.NewObjectID(),
		Token:     "expired",
		ExpiresAt: time.Now().Add(-time.Minute),
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.FindByToken(ctx, "expired"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	// FindByToken deletes on expiry; a second lookup should say not-found.
	if _, err := repo.FindByToken(ctx, "expired"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session was not cleaned up; err = %v", err)
	}
}

// This is the query behind login rotation: keep the new session, drop the
// user's others, and never touch another user's sessions.
func TestSessionDeleteByUserIDExceptToken(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	victim := primitive.NewObjectID()
	bystander := primitive.NewObjectID()
	future := time.Now().Add(time.Hour)

	for _, tok := range []string{"old-1", "old-2", "keep-me"} {
		if err := repo.Create(ctx, &models.Session{UserID: victim, Token: tok, ExpiresAt: future}); err != nil {
			t.Fatalf("Create %s: %v", tok, err)
		}
	}
	if err := repo.Create(ctx, &models.Session{UserID: bystander, Token: "other-user", ExpiresAt: future}); err != nil {
		t.Fatalf("Create bystander: %v", err)
	}

	if err := repo.DeleteByUserIDExceptToken(ctx, victim, "keep-me"); err != nil {
		t.Fatalf("DeleteByUserIDExceptToken: %v", err)
	}

	if _, err := repo.FindByToken(ctx, "keep-me"); err != nil {
		t.Errorf("the kept session was deleted: %v", err)
	}
	for _, tok := range []string{"old-1", "old-2"} {
		if _, err := repo.FindByToken(ctx, tok); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("session %s survived rotation: %v", tok, err)
		}
	}
	if _, err := repo.FindByToken(ctx, "other-user"); err != nil {
		t.Errorf("another user's session was destroyed: %v", err)
	}
}

// The query behind "revoke everything after a password change".
func TestSessionDeleteByUserID(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	victim := primitive.NewObjectID()
	bystander := primitive.NewObjectID()
	future := time.Now().Add(time.Hour)

	for _, tok := range []string{"a", "b"} {
		_ = repo.Create(ctx, &models.Session{UserID: victim, Token: tok, ExpiresAt: future})
	}
	_ = repo.Create(ctx, &models.Session{UserID: bystander, Token: "safe", ExpiresAt: future})

	if err := repo.DeleteByUserID(ctx, victim); err != nil {
		t.Fatalf("DeleteByUserID: %v", err)
	}
	sessions, err := repo.FindByUserID(ctx, victim)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("%d sessions survived revocation", len(sessions))
	}
	if _, err := repo.FindByToken(ctx, "safe"); err != nil {
		t.Errorf("another user's session was revoked: %v", err)
	}
}

// FindByUserID filters on expiry server-side; a stale row must not come back.
func TestSessionFindByUserIDExcludesExpired(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewSessionRepository(db)

	uid := primitive.NewObjectID()
	_ = repo.Create(ctx, &models.Session{UserID: uid, Token: "live", ExpiresAt: time.Now().Add(time.Hour)})
	_ = repo.Create(ctx, &models.Session{UserID: uid, Token: "stale", ExpiresAt: time.Now().Add(-time.Hour)})

	sessions, err := repo.FindByUserID(ctx, uid)
	if err != nil {
		t.Fatalf("FindByUserID: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Token != "live" {
		t.Errorf("got %d sessions %v, want only the live one", len(sessions), sessions)
	}
}

// --- UserRepository ---

func TestUserCreateAndFindByEmail(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	u := &models.User{Email: "ada@example.com", PasswordHash: "hash", Name: "Ada"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FindByEmail(ctx, "ada@example.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("Name = %q", got.Name)
	}

	byID, err := repo.FindByID(ctx, got.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.Email != "ada@example.com" {
		t.Errorf("Email = %q", byID.Email)
	}
}

func TestUserFindByEmailUnknown(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	if _, err := repo.FindByEmail(ctx, "nobody@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

// Registration relies on the unique index to prevent duplicate accounts;
// AuthService maps ErrEmailExists onto "email already registered".
func TestUserCreateDuplicateEmailIsRejected(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	first := &models.User{Email: "dup@example.com", PasswordHash: "h", Name: "First"}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	second := &models.User{Email: "dup@example.com", PasswordHash: "h", Name: "Second"}
	err := repo.Create(ctx, second)
	if err == nil {
		t.Fatal("duplicate email was accepted; the unique index is not protecting registration")
	}
	if !errors.Is(err, ErrEmailExists) {
		t.Errorf("err = %v, want ErrEmailExists so the handler can report it cleanly", err)
	}
}

func TestUserUpdatePassword(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	u := &models.User{Email: "pw@example.com", PasswordHash: "old", Name: "PW"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdatePassword(ctx, u.ID, "new-hash"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	got, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.PasswordHash != "new-hash" {
		t.Errorf("PasswordHash = %q, want new-hash", got.PasswordHash)
	}
}

func TestUserUpdateRole(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	u := &models.User{Email: "role@example.com", PasswordHash: "h", Name: "R"}
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateRole(ctx, u.ID, "admin"); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	got, _ := repo.FindByID(ctx, u.ID)
	if !got.IsAdmin {
		t.Error("UpdateRole(admin) did not set IsAdmin")
	}
}

func TestUserCountAndList(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewUserRepository(db)

	for _, e := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if err := repo.Create(ctx, &models.User{Email: e, PasswordHash: "h", Name: e}); err != nil {
			t.Fatalf("Create %s: %v", e, err)
		}
	}
	n, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 3 {
		t.Errorf("Count = %d, want 3", n)
	}

	page1, total, err := repo.List(ctx, 1, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(page1) != 2 {
		t.Errorf("page 1 has %d users, want 2", len(page1))
	}
	page2, _, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Errorf("page 2 has %d users, want 1", len(page2))
	}
}

// --- ListingRepository.Search ---
//
// The filters are unit-tested; this proves the resulting query documents
// actually select the right rows on a server.

func TestListingSearchFiltersAgainstServer(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewListingRepository(db)

	// URL must be unique — listings carries a unique index on it, so every
	// fixture needs its own or the seed fails on the second insert.
	docs := []models.Listing{
		{URL: "https://example.test/1", Title: "Lake cabin", Location: "Detroit, Michigan", Region: "Midwest", Country: "USA",
			PriceNumeric: 120, RatingNumeric: 4.8, Features: []string{"wifi", "pool"}, PropertyType: "Cabin"},
		{URL: "https://example.test/2", Title: "City loft", Location: "Detroit Lakes, Minnesota", Region: "Midwest", Country: "USA",
			PriceNumeric: 300, RatingNumeric: 4.2, Features: []string{"wifi"}, PropertyType: "Loft"},
		{URL: "https://example.test/3", Title: "Beach house", Location: "Nice, France", Region: "Europe", Country: "France",
			PriceNumeric: 250, RatingNumeric: 3.9, Features: []string{"pool"}, PropertyType: "House"},
	}
	for i := range docs {
		if _, err := db.Collection("listings").InsertOne(ctx, docs[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	t.Run("city filter is anchored", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{City: "Detroit"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// "Detroit" must match "Detroit, Michigan" but not "Detroit Lakes".
		if res.Total != 1 {
			t.Fatalf("total = %d, want 1 (Detroit Lakes must not match)", res.Total)
		}
		if len(res.Listings) != 1 || res.Listings[0].Title != "Lake cabin" {
			t.Errorf("got %+v", res.Listings)
		}
	})

	t.Run("price range", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{MinPrice: 200, MaxPrice: 280})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Total != 1 || res.Listings[0].Title != "Beach house" {
			t.Errorf("total=%d listings=%+v", res.Total, res.Listings)
		}
	})

	t.Run("features are conjunctive", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{Features: []string{"wifi", "pool"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Total != 1 || res.Listings[0].Title != "Lake cabin" {
			t.Errorf("$all should require both features; got total=%d %+v", res.Total, res.Listings)
		}
	})

	t.Run("min rating", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{MinRating: 4.5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Total != 1 {
			t.Errorf("total = %d, want 1", res.Total)
		}
	})

	t.Run("text query spans fields", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{Query: "france"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// Matches on location, case-insensitively.
		if res.Total != 1 || res.Listings[0].Title != "Beach house" {
			t.Errorf("total=%d %+v", res.Total, res.Listings)
		}
	})

	t.Run("sort by price ascending", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{SortBy: "price_asc"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Listings) != 3 {
			t.Fatalf("got %d listings", len(res.Listings))
		}
		for i := 1; i < len(res.Listings); i++ {
			if res.Listings[i-1].PriceNumeric > res.Listings[i].PriceNumeric {
				t.Errorf("not ascending: %v", res.Listings)
				break
			}
		}
	})

	t.Run("pagination", func(t *testing.T) {
		p1, err := repo.Search(ctx, models.ListingSearchParams{Page: 1, Limit: 2, SortBy: "price_asc"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if p1.Total != 3 || len(p1.Listings) != 2 {
			t.Errorf("page1 total=%d len=%d", p1.Total, len(p1.Listings))
		}
		p2, err := repo.Search(ctx, models.ListingSearchParams{Page: 2, Limit: 2, SortBy: "price_asc"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(p2.Listings) != 1 {
			t.Errorf("page2 len=%d, want 1", len(p2.Listings))
		}
		if len(p1.Listings) > 0 && len(p2.Listings) > 0 && p1.Listings[0].Title == p2.Listings[0].Title {
			t.Error("page 2 repeated page 1")
		}
	})

	t.Run("no filters returns everything", func(t *testing.T) {
		res, err := repo.Search(ctx, models.ListingSearchParams{})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Total != 3 {
			t.Errorf("total = %d, want 3", res.Total)
		}
	})
}
