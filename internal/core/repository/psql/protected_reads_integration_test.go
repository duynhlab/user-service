//go:build integration

package psql

import (
	"context"
	"testing"
)

// TestSearchProfiles_Integration proves the operator search over the real
// schema (RFC-0023): name/phone ILIKE, exact user_id, paging + total.
func TestSearchProfiles_Integration(t *testing.T) {
	pool := newTestDB(t)
	repo := NewUserRepository(pool)
	ctx := context.Background()

	seed := []struct{ id, first, last, phone string }{
		{"sp-u1", "Zaphrina", "Beeble", "555-870001"},
		{"sp-u2", "Zaphod", "Brox", "555-870002"},
		{"sp-u3", "Quorra", "Flynn", "777-87999"},
	}
	for _, u := range seed {
		if err := repo.UpsertUserProfile(ctx, u.id, u.first, u.last, u.phone); err != nil {
			t.Fatalf("seed %s: %v", u.id, err)
		}
	}

	// Case-insensitive name match hits both Zaph* profiles (unique tokens —
	// the harness DB may carry demo seeds).
	items, total, err := repo.SearchProfiles(ctx, "zaph", 10, 0)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("name search = (%d items, total %d, %v), want 2", len(items), total, err)
	}

	// Phone fragment.
	items, total, err = repo.SearchProfiles(ctx, "777-87", 10, 0)
	if err != nil || total != 1 || *items[0].FirstName != "Quorra" {
		t.Fatalf("phone search = (%v, total %d, %v)", items, total, err)
	}

	// Exact user_id (no fuzz).
	items, total, err = repo.SearchProfiles(ctx, "sp-u1", 10, 0)
	if err != nil || total != 1 || items[0].UserID != "sp-u1" {
		t.Fatalf("id search = (%v, total %d, %v)", items, total, err)
	}
	if items[0].CreatedAt.IsZero() {
		t.Fatalf("timestamps must scan, got zero CreatedAt")
	}

	// Paging: page 2 of the unfiltered set differs from page 1.
	p1, allTotal, err := repo.SearchProfiles(ctx, "", 1, 0)
	if err != nil || allTotal < 3 {
		t.Fatalf("page1 = (%v, total %d, %v)", p1, allTotal, err)
	}
	p2, _, err := repo.SearchProfiles(ctx, "", 1, 1)
	if err != nil || len(p2) != 1 || p2[0].UserID == p1[0].UserID {
		t.Fatalf("paging broken: %v vs %v (%v)", p1, p2, err)
	}
}
