package configspec

import "testing"

// TestVarsAreWellFormed catches the four mistakes you'd plausibly make
// editing Vars by hand: duplicate names, empty names, unknown scope, or
// unknown kind. Each one would produce a wrong-but-not-obviously-broken
// configuration.md without this guard.
func TestVarsAreWellFormed(t *testing.T) {
	validScopes := map[Scope]bool{
		ScopeCollector: true,
		ScopeDashboard: true,
		ScopeShared:    true,
	}
	validKinds := map[Kind]bool{
		KindString:     true,
		KindBool:       true,
		KindDuration:   true,
		KindBytes:      true,
		KindStringList: true,
	}

	seen := make(map[string]int, len(Vars))
	for i, v := range Vars {
		if v.Name == "" {
			t.Errorf("Vars[%d]: empty Name", i)
		}
		if prev, ok := seen[v.Name]; ok {
			t.Errorf("Vars[%d] (%q): duplicate Name, first seen at index %d", i, v.Name, prev)
		}
		seen[v.Name] = i
		if !validScopes[v.Scope] {
			t.Errorf("Vars[%d] (%q): unknown Scope %q", i, v.Name, v.Scope)
		}
		if !validKinds[v.Kind] {
			t.Errorf("Vars[%d] (%q): unknown Kind %q", i, v.Name, v.Kind)
		}
		if v.Description == "" {
			t.Errorf("Vars[%d] (%q): empty Description", i, v.Name)
		}
	}
}

func TestByNameRoundTrip(t *testing.T) {
	for _, want := range Vars {
		got, ok := ByName(want.Name)
		if !ok {
			t.Errorf("ByName(%q) returned !ok for a var that's in Vars", want.Name)
			continue
		}
		if got.Name != want.Name {
			t.Errorf("ByName(%q).Name = %q", want.Name, got.Name)
		}
	}

	if _, ok := ByName("PROVENANCE_NOT_A_REAL_VAR"); ok {
		t.Error("ByName returned ok for a name not in Vars")
	}
}
