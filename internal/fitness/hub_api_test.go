package fitness

import "testing"

func TestValidWebCallbackAcceptsOnlyTheCallbackPage(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"https://kheprios.com/app/fitness/strava/callback":         true,
		"http://localhost:5173/app/fitness/strava/callback":        true,
		"https://kheprios.com/app/fitness/strava/callback?x=1":     false,
		"https://kheprios.com/app/fitness/strava/callback#frag":    false,
		"https://kheprios.com/app/settings":                        false,
		"javascript:alert(1)":                                      false,
		"/app/fitness/strava/callback":                             false,
		"https://user:pw@kheprios.com/app/fitness/strava/callback": false,
		"khepri://fitness/strava":                                  false,
	}
	for raw, want := range cases {
		if got := validWebCallback(raw); got != want {
			t.Errorf("validWebCallback(%q) = %v, want %v", raw, got, want)
		}
	}
}
