// Package fitness is the /app/fitness hub page: one landing page linking out
// to Training, Form check, Activity, and the Calculator, since those each
// live in their own feature slice with their own routes. It owns no data.
//
// Future provider integrations (Strava, Apple Health, Garmin) still belong
// here once built — the hub and the provider adapters are not in tension,
// this package just does double duty until there's a provider to wire in.
//
// TODO: Strava is still the only provider a person can actually connect. The
// hub reads internal/health as well, but that data has to be pushed from a
// phone rather than fetched, so there is no second "Connect" button anywhere.
//
// The providers a web app can genuinely reach are the ones with cloud APIs:
// Whoop and Withings have open developer portals, Polar has AccessLink, Oura
// has a v2 API (users report it sitting behind a paid tier), and Garmin needs
// approval plus a commercial licence. Each mirrors the Strava flow almost
// exactly — authorize URL, callback, sealed tokens, a sync job — so
// internal/fitness/strava is the template, not a thing to abstract over first.
//
// Deliberately not doing: Fitbit, whose Web API is being sunset, and Google
// Fit, which is being turned off. Both would be dead on arrival.
//
// A fitness.Provider interface is the obvious next refactor and is on purpose
// not built yet: with one real provider it would be an abstraction fitted to a
// single example. Write the second one concretely, then extract what the two
// actually share.
package fitness
