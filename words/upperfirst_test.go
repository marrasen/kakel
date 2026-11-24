package words

import "testing"

func TestUpperFirstLeavesKakelAsItIs(t *testing.T) {
	for in, want := range map[string]string{
		"":                         "",
		"lost":                     "Lost",
		"kakel is not connected":   "kakel is not connected",
		"kakel":                    "kakel",
		"kakels on other machines": "Kakels on other machines",
	} {
		if got := UpperFirst(in); got != want {
			t.Errorf("UpperFirst(%q) is %q, want %q", in, got, want)
		}
	}
}
