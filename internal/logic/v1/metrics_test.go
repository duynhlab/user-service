package v1

import (
	"context"
	"strconv"
	"testing"

	"github.com/duynhlab/user-service/internal/core/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// counterByLabel reads name into a label-value→sum map keyed by a single label.
func counterByLabel(t *testing.T, reader sdkmetric.Reader, name, label string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s is %T, want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key(label))
				out[v.AsString()] = dp.Value
			}
		}
	}
	return out
}

// lookupByAudienceFound reads user.profile_lookup.total into an
// "audience/found"→sum map so both bounded labels are asserted together.
func lookupByAudienceFound(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "user.profile_lookup.total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			for _, dp := range sum.DataPoints {
				aud, _ := dp.Attributes.Value(attribute.Key("audience"))
				found, _ := dp.Attributes.Value(attribute.Key("found"))
				out[aud.AsString()+"/"+strconv.FormatBool(found.AsBool())] = dp.Value
			}
		}
	}
	return out
}

// svcWith builds a UserService over a mockRepo wired with the given behavior.
func svcWith(m *mockRepo) *UserService { return &UserService{repo: m} }

// wantOK fails the test if err is non-nil; wantErr fails if err is nil.
func wantOK(t *testing.T, msg string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", msg, err)
	}
}

func wantErr(t *testing.T, msg string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error, got nil", msg)
	}
}

// TestProfileMetrics drives every recording branch of both W2 counters on one
// ManualReader-backed MeterProvider. It is intentionally NOT parallel: the OTel
// global provider is first-wins, so exactly one SetMeterProvider per test binary
// takes effect, and running in the sequential phase (before the package's
// t.Parallel() tests resume) keeps the asserted cumulative counts clean.
func TestProfileMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	ctx := context.Background()

	// --- profile_updated: success + unauthorized, and the not-counted DB error.
	updOK := svcWith(&mockRepo{upsertUserProfileFn: func(_ context.Context, _ int, _, _, _ string) error { return nil }})
	_, err := updOK.UpdateProfile(ctx, "1", domain.UpdateProfileRequest{Name: "Alice Johnson"})
	wantOK(t, "update success", err)
	// Non-numeric user id → authz rejection, never reaches the repo.
	_, err = updOK.UpdateProfile(ctx, "abc", domain.UpdateProfileRequest{Name: "x"})
	wantErr(t, "update unauthorized", err)
	// Upsert failure is an internal error → must NOT touch the business counter.
	updErr := svcWith(&mockRepo{upsertUserProfileFn: func(_ context.Context, _ int, _, _, _ string) error { return errRepo }})
	_, err = updErr.UpdateProfile(ctx, "2", domain.UpdateProfileRequest{Name: "y"})
	wantErr(t, "update repo error", err)

	upd := counterByLabel(t, reader, "user.profile_updated.total", "result")
	assertCounts(t, "profile_updated", upd, map[string]int64{resultSuccess: 1, resultUnauthorized: 1})
	if total := upd[resultSuccess] + upd[resultUnauthorized]; total != 2 {
		t.Errorf("profile_updated total = %d, want 2 (DB error must not be counted)", total)
	}

	// --- profile_lookup public: hit, miss (404), and the not-counted internal error.
	pubHit := svcWith(&mockRepo{getUserFn: func(_ context.Context, id string) (*domain.User, error) {
		return &domain.User{ID: id, Name: "Alice"}, nil
	}})
	_, err = pubHit.GetUser(ctx, "1")
	wantOK(t, "public hit", err)
	pubMiss := svcWith(&mockRepo{getUserFn: func(_ context.Context, _ string) (*domain.User, error) {
		return nil, domain.ErrUserNotFound
	}})
	_, err = pubMiss.GetUser(ctx, "999")
	wantErr(t, "public miss", err)
	pubErr := svcWith(&mockRepo{getUserFn: func(_ context.Context, _ string) (*domain.User, error) { return nil, errRepo }})
	_, err = pubErr.GetUser(ctx, "1")
	wantErr(t, "public internal error", err)

	// --- profile_lookup private: hit (stored row) + miss (auth fallback).
	privHit := svcWith(&mockRepo{getProfileByUserIDFn: func(_ context.Context, _ int) (*domain.UserProfile, error) {
		return &domain.UserProfile{FirstName: strPtr("Alice")}, nil
	}})
	_, err = privHit.GetProfile(ctx, "1", "alice", "alice@example.com")
	wantOK(t, "private hit", err)
	privMiss := svcWith(&mockRepo{getProfileByUserIDFn: func(_ context.Context, _ int) (*domain.UserProfile, error) {
		return nil, nil
	}})
	_, err = privMiss.GetProfile(ctx, "2", "bob", "bob@example.com")
	wantOK(t, "private miss", err)

	look := lookupByAudienceFound(t, reader)
	assertCounts(t, "profile_lookup", look, map[string]int64{
		audiencePublic + "/true":   1,
		audiencePublic + "/false":  1,
		audiencePrivate + "/true":  1,
		audiencePrivate + "/false": 1,
	})
	// Internal read error must not have added a public data point.
	if total := look[audiencePublic+"/true"] + look[audiencePublic+"/false"]; total != 2 {
		t.Errorf("public lookup total = %d, want 2 (internal error must not be counted)", total)
	}
}

// assertCounts checks each want[key] against got[key].
func assertCounts(t *testing.T, metricName string, got, want map[string]int64) {
	t.Helper()
	for key, w := range want {
		if got[key] != w {
			t.Errorf("%s{%s} = %d, want %d", metricName, key, got[key], w)
		}
	}
}
