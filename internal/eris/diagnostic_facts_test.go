package eris

import (
	"math"
	"testing"
)

func TestDiagnosticNarrationOnlyAcceptsBoundedKnownObservations(t *testing.T) {
	base := DiagnosticFacts{Title: "Toy Story 5", Stage: "manual_import", PlexIndex: "indexed"}
	if !base.Valid() {
		t.Fatal("known diagnostic rejected")
	}
	for _, change := range []func(*DiagnosticFacts){
		func(d *DiagnosticFacts) { d.Stage = "run-shell" }, func(d *DiagnosticFacts) { d.PlexIndex = "playable" }, func(d *DiagnosticFacts) { d.Title = "bad\ncontrol" },
		func(d *DiagnosticFacts) { v := 101.0; d.ProgressPercent = &v }, func(d *DiagnosticFacts) { v := math.NaN(); d.ProgressPercent = &v }, func(d *DiagnosticFacts) { v := -1; d.MissingEpisodes = &v },
	} {
		d := base
		change(&d)
		if d.Valid() {
			t.Fatal("invalid observation accepted", d)
		}
	}
}
