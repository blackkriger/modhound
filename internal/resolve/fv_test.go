package resolve

import "testing"

func TestFileVersion(t *testing.T) {
	cases := map[string]string{
		"weaponmod-forge-1.7.10-1.25.0.jar":                  "1.25.0",
		"Mantle-1.7.10-0.3.2b.jar":                           "0.3.2b",
		"growthcraft-1.7.10-2.7.3-complete.jar":              "2.7.3",
		"Mekanism-Community-Edition-1.7.10-9.10.51-Core.jar": "9.10.51",
		"SpmodAPI Version 1.0.2.6.jar":                       "1.0.2.6",
		"salisarcana-v2.7.0.jar":                             "v2.7.0",
		"world-tooltips-1.2.3-87.jar":                        "1.2.3-87",
		"SlashBlade-mc1.7.10-r88.jar":                        "r88",
		"[1.7.10]Treecapitator-universal-2.0.4.jar":          "2.0.4",
		"CoFHCore-[1.7.10]3.1.4-329.jar":                     "3.1.4-329",
	}
	for in, want := range cases {
		if got := FileVersion(in, "1.7.10"); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestSameVersion(t *testing.T) {
	if !sameVersion("SpmodAPI.Version.1.0.2.6.jar", "SpmodAPI Version 1.0.2.6.jar", "1.7.10") {
		t.Error("reupload with a renamed file must be the same version")
	}
	if sameVersion("Mantle-1.7.10-0.3.2.jar", "Mantle-1.7.10-0.3.2b.jar", "1.7.10") {
		t.Error("a letter suffix is a different version")
	}
}

func TestStemKeepsShortPrefixNames(t *testing.T) {
	for in, want := range map[string]string{
		"IC2NuclearControl-2.4.jar":   "ic2nuclearcontrol",
		"ae2stuff-0.10.19-GTNH.jar":   "ae2stuff gtnh",
		"SlashBlade-mc1.7.10-r88.jar": "slashblade",
		"Mantle-1.7.10-0.3.2b.jar":    "mantle",
	} {
		if got := Stem(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestOlderVersion(t *testing.T) {
	if !OlderVersion("Mod-1.7.10-1.9.9.jar", "Mod-1.7.10-2.0.1.jar", "1.7.10") {
		t.Error("1.9.9 is older than 2.0.1")
	}
	if OlderVersion("Mod-1.7.10-2.0.10.jar", "Mod-1.7.10-2.0.9.jar", "1.7.10") {
		t.Error("2.0.10 is newer than 2.0.9")
	}
	if OlderVersion("Mantle-1.7.10-0.3.2b.jar", "Mantle-1.7.10-0.3.2.jar", "1.7.10") {
		t.Error("non-numeric versions are not compared")
	}
}
