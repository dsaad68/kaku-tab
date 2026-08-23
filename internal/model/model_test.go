// SPDX-License-Identifier: MIT

package model

import "testing"

// A satellite is recognised purely by its name, so these rules decide whether a
// session's windows are listed, whether its client counts towards the base, and
// whether a rename carries it along. Getting IsSatellite wrong duplicates every
// row of a grouped session, or orphans a tab from the session it belongs to.
func TestIsSatellite(t *testing.T) {
	const sfx = DefaultSatelliteSuffix
	cases := map[string]bool{
		"api~kaku2":  true,
		"api~kaku64": true,
		"~kaku2":     true, // an empty base is still a satellite name
		"api":        false,
		"api~kaku":   false, // the suffix alone is not enough; an index must follow
		"api~kakux":  false, // ... and it has to be a digit
		"api~kaku ":  false,
		"kaku2":      false, // no suffix at all
		"":           false,
	}
	for name, want := range cases {
		if got := IsSatellite(name, sfx); got != want {
			t.Errorf("IsSatellite(%q) = %v, want %v", name, got, want)
		}
	}
}

// An empty suffix disables the whole notion. Without this guard strings.Index
// returns 0 for every session and the picker would hide all of them.
func TestIsSatelliteEmptySuffixMatchesNothing(t *testing.T) {
	for _, name := range []string{"api", "api~kaku2", ""} {
		if IsSatellite(name, "") {
			t.Errorf("IsSatellite(%q, \"\") reported a satellite", name)
		}
	}
}

func TestBaseSession(t *testing.T) {
	const sfx = DefaultSatelliteSuffix
	cases := map[string]string{
		"api~kaku2":  "api",
		"api~kaku12": "api",
		"~kaku2":     "",
		"api":        "api",      // not a satellite: returned unchanged
		"api~kaku":   "api~kaku", // ... and neither is this
		"":           "",
	}
	for name, want := range cases {
		if got := BaseSession(name, sfx); got != want {
			t.Errorf("BaseSession(%q) = %q, want %q", name, got, want)
		}
	}
}

// A satellite is tied to its base by name alone, so the two functions have to
// agree: anything IsSatellite accepts, BaseSession must reduce to a shorter
// name, or a rename would leave the satellite pointing at a session that is not
// there.
func TestBaseSessionAgreesWithIsSatellite(t *testing.T) {
	const sfx = DefaultSatelliteSuffix
	for _, name := range []string{"api~kaku2", "web~kaku9", "a~kaku2"} {
		base := BaseSession(name, sfx)
		if base == name {
			t.Errorf("%q is a satellite but BaseSession left it unchanged", name)
		}
		if IsSatellite(base, sfx) {
			t.Errorf("BaseSession(%q) = %q, which is itself a satellite", name, base)
		}
	}
}

// Indices start at 2: the base session is the first tab, so its first satellite
// is the second.
func TestNextSatelliteStartsAtTwo(t *testing.T) {
	got := NextSatellite("api", DefaultSatelliteSuffix, func(string) bool { return false })
	if want := "api~kaku2"; got != want {
		t.Errorf("NextSatellite = %q, want %q", got, want)
	}
}

func TestNextSatelliteSkipsTaken(t *testing.T) {
	taken := map[string]bool{"api~kaku2": true, "api~kaku3": true, "api~kaku5": true}
	got := NextSatellite("api", DefaultSatelliteSuffix, func(s string) bool { return taken[s] })
	if want := "api~kaku4"; got != want {
		t.Errorf("NextSatellite = %q, want %q", got, want)
	}
}

// The search is bounded, so an exhausted range has to yield something rather
// than loop or return an empty name that would collide with the base session.
func TestNextSatelliteExhausted(t *testing.T) {
	got := NextSatellite("api", DefaultSatelliteSuffix, func(string) bool { return true })
	if want := "api~kakux"; got != want {
		t.Errorf("NextSatellite when everything is taken = %q, want %q", got, want)
	}
	if got == "api" {
		t.Error("fell back to the base session name")
	}
}

// Whatever NextSatellite hands out must read back as a satellite, or the picker
// will list the new session's windows a second time.
func TestNextSatelliteProducesRecognisableNames(t *testing.T) {
	const sfx = DefaultSatelliteSuffix
	n := 0
	name := NextSatellite("api", sfx, func(string) bool {
		n++
		return n < 40 // force it deep into the range
	})
	if !IsSatellite(name, sfx) {
		t.Errorf("NextSatellite produced %q, which IsSatellite rejects", name)
	}
	if BaseSession(name, sfx) != "api" {
		t.Errorf("BaseSession(%q) = %q, want api", name, BaseSession(name, sfx))
	}
}

// These strings are the machine-readable output of `kaku-tab resolve`.
func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		Visible:        "VISIBLE",
		AttachedHidden: "ATTACHED_HIDDEN",
		Detached:       "DETACHED",
		Status(99):     "DETACHED", // an unknown status must not print blank
	}
	for st, want := range cases {
		if got := st.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(st), got, want)
		}
	}
}

// Status is ordered, and both the resolver and the tree header rollup rely on
// it: "the best status among these windows" is a numeric comparison.
func TestStatusOrdering(t *testing.T) {
	if Detached >= AttachedHidden || AttachedHidden >= Visible {
		t.Error("Status constants are not ordered least- to most-present")
	}
}
