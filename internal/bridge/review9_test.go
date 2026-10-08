package bridge

import "testing"

// Review 9: a non-ASCII extra_deny pattern can't be matched against every
// spelling the filesystem accepts, so it is refused at startup.
func TestExtraDenyRejectsNonASCII(t *testing.T) {
	for _, g := range []string{"résumé.txt", "re\u0301sume\u0301.txt", "ſecret*", "naïve*"} {
		c := Config{BridgeTokenSHA256: HashToken("x"), ExtraDeny: []string{g}}
		if err := c.Validate(); err == nil {
			t.Errorf("extra_deny %q accepted", g)
		}
	}
	c := Config{BridgeTokenSHA256: HashToken("x"), ExtraDeny: []string{"r*sum*.txt", "DRAFT*"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("ASCII patterns refused: %v", err)
	}
	// The ASCII workaround catches both spellings of the accented name.
	fsys, _ := NewFS(nil, []string{"r*sum*.txt"})
	for _, name := range []string{"résumé.txt", "re\u0301sume\u0301.txt", "RÉSUMÉ.TXT"} {
		if !fsys.denied(name) {
			t.Errorf("r*sum*.txt did not deny %q", name)
		}
	}
}
