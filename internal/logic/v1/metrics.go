package v1

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Business metrics for user-service (RFC-0017 W2), answering the on-call
// questions that matter for the profile surface:
//  1. Are profile updates being rejected on authorization?  → profile_updated{result}
//  2. What is the profile read hit/miss rate per audience?   → profile_lookup{audience,found}
//
// Instruments ride the global OTel MeterProvider that obsx.SetupObservability
// installs (RFC-0014 OTLP pipeline → collector → VictoriaMetrics). Before that
// setup the global provider is a no-op, so package-init here is safe. Names are
// OTel-style; the collector renders them as user_profile_updated_total and
// user_profile_lookup_total.
//
// Labels are bounded to enumerable domain values (RFC-0017 D-9): no user ids,
// usernames, emails, or other PII — only the fixed enums below.
var (
	meter = otel.Meter("user-service")

	profileUpdatedCounter, _ = meter.Int64Counter("user.profile_updated.total",
		metric.WithDescription("Profile-update attempts by outcome (successful write vs authz rejection)"))
	profileLookupCounter, _ = meter.Int64Counter("user.profile_lookup.total",
		metric.WithDescription("Profile-lookup reads by caller audience and hit/miss"))
)

// Profile-update outcomes (bounded).
const (
	resultSuccess      = "success"
	resultUnauthorized = "unauthorized"
)

// Lookup audiences (bounded) — which endpoint issued the read.
const (
	audiencePublic  = "public"
	audiencePrivate = "private"
)

// recordProfileUpdated counts one profile-update outcome. Called exactly once
// per UpdateProfile invocation on a terminal branch: resultUnauthorized when the
// caller identity cannot be resolved, resultSuccess after the upsert lands.
// Persistence failures are NOT counted here — they surface via the otelpgx DB
// span and pool error signals, mirroring the payment-service convention.
func recordProfileUpdated(ctx context.Context, result string) {
	profileUpdatedCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("result", result),
	))
}

// recordProfileLookup counts one profile-read outcome. found reflects whether a
// stored profile row was located: on the public endpoint a miss is a 404, on the
// private endpoint a miss returns the auth-derived fallback (still HTTP 200).
// Internal read failures are not counted — they surface via the DB span.
func recordProfileLookup(ctx context.Context, audience string, found bool) {
	profileLookupCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("audience", audience),
		attribute.Bool("found", found),
	))
}
