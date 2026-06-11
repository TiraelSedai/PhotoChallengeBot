package history

import (
	"strings"
	"testing"
)

func TestParseUserIDsSkipsNonNumericAndConflicts(t *testing.T) {
	t.Parallel()

	fixture := `freilin,168659798
@Sergio_Luponi,60023768
maximumquiet,n/a
freilin,999
`
	ids, warnings, err := ParseUserIDs(strings.NewReader(fixture))
	if err != nil {
		t.Fatalf("ParseUserIDs() error = %v", err)
	}

	if got := ids["freilin"]; got != 168659798 {
		t.Fatalf("ids[freilin] = %d, want first mapping 168659798 kept", got)
	}
	if got := ids["sergio_luponi"]; got != 60023768 {
		t.Fatalf("ids[sergio_luponi] = %d, want lowercase key without @", got)
	}
	if _, ok := ids["maximumquiet"]; ok {
		t.Fatal("ids contains maximumquiet, want n/a row skipped")
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2 entries", ids)
	}

	for _, want := range []string{"n/a", "conflicting user ids"} {
		found := false
		for _, warning := range warnings {
			if strings.Contains(warning, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("warnings = %v, want one containing %q", warnings, want)
		}
	}
}

func TestParseUserIDsFailsWithoutMappings(t *testing.T) {
	t.Parallel()

	if _, _, err := ParseUserIDs(strings.NewReader("only_na,n/a\n")); err == nil {
		t.Fatal("ParseUserIDs() expected error when no mappings found")
	}
}
