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
		t.Error("0.3.2b is newer than 0.3.2")
	}
	for _, c := range [][2]string{
		{"appliedenergistics2-rv3-beta-926-GTNH.jar", "appliedenergistics2-rv3-beta-1096-GTNH.jar"},
		{"HBM-NTM-[1.0.27_X5687].jar", "HBM-NTM-.1.0.27_X5863.jar"},
		{"PackagedAuto-1.7.10-W.0.2.8.jar", "PackagedAuto-1.7.10-W.0.2.9.jar"},
		{"Mantle-1.7.10-0.3.2.jar", "Mantle-1.7.10-0.3.2b.jar"},
		{"ae2_auto_pattern_upload-1.1.3.jar", "ae2_auto_pattern_upload-2.0.0-GTNH_290.jar"},
		{"Mod-1.7.10-2.0-beta1.jar", "Mod-1.7.10-2.0.jar"},
		{"Mod-1.7.10-2.0-beta2.jar", "Mod-1.7.10-2.0-rc1.jar"},
	} {
		if !OlderVersion(c[0], c[1], "1.7.10") || OlderVersion(c[1], c[0], "1.7.10") {
			t.Errorf("%s must be older than %s", c[0], c[1])
		}
	}
	for _, c := range [][2]string{
		{"Mod-1.7.10-build-a.jar", "Mod-1.7.10-build-b.jar"},
		{"Mod-1.7.10-r12.jar", "Mod-1.7.10-12.jar"},
	} {
		if _, ok := CompareVersions(c[0], c[1], "1.7.10"); ok {
			t.Errorf("%s and %s must not be comparable", c[0], c[1])
		}
	}
}

func TestModrinthSide(t *testing.T) {
	cases := [][3]string{
		{"required", "required", "both"},
		{"optional", "required", "both"},
		{"required", "optional", ""},
		{"optional", "optional", ""},
		{"required", "unsupported", "client"},
		{"unsupported", "required", "server"},
		{"required", "unknown", ""},
		{"unknown", "unknown", ""},
	}
	for _, c := range cases {
		if got := modrinthSide(c[0], c[1]); got != c[2] {
			t.Fatalf("modrinthSide(%s, %s) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestPreReleaseAndShortMinecraftVersions(t *testing.T) {
	for _, c := range [][2]string{
		{"Mod-1.7.10-2.0-beta1.jar", "Mod-1.7.10-2.0.jar"},
		{"Mod-1.7.10-2.0-beta.jar", "Mod-1.7.10-2.0.jar"},
		{"Mod-1.7.10-2.0-beta-2.jar", "Mod-1.7.10-2.0.jar"},
		{"Mod-1.7.10-2.0-rc.jar", "Mod-1.7.10-2.0.jar"},
		{"Mod-1.7-1.1.jar", "Mod-1.7.10-1.2.jar"},
	} {
		if Stem(c[0]) != Stem(c[1]) {
			t.Errorf("%s and %s must share a stem, got %q and %q", c[0], c[1], Stem(c[0]), Stem(c[1]))
		}
		if !OlderVersion(c[0], c[1], "1.7.10") || sameVersion(c[0], c[1], "1.7.10") {
			t.Errorf("%s must be older than %s", c[0], c[1])
		}
	}
	if !sameVersion("Mod-2.0.jar", "Mod-2.0.0.jar", "1.7.10") || OlderVersion("Mod-2.0.jar", "Mod-2.0.0.jar", "1.7.10") {
		t.Error("2.0 and 2.0.0 are the same version")
	}
	if Stem("Mod-1.7.10-2.0-dev.jar") == Stem("Mod-1.7.10-2.0.jar") {
		t.Error("a dev jar is a variant, not the same file")
	}
	if got := FileVersion("Mod-1.7.jar", "1.7.10"); got != "1.7" {
		t.Errorf("a lone 1.7 is the mod version, got %q", got)
	}
}
