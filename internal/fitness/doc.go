// Package fitness is the /app/fitness hub page: one landing page linking out
// to Training, Form check, Activity, and the Calculator, since those each
// live in their own feature slice with their own routes. It owns no data.
//
// Future provider integrations (Strava, Apple Health, Garmin) still belong
// here once built — the hub and the provider adapters are not in tension,
// this package just does double duty until there's a provider to wire in.
package fitness
