package resolve

import (
	"testing"

	"github.com/blackkriger/modhound/internal/github"
	"github.com/blackkriger/modhound/internal/jarinfo"
)

func TestKnownRepo(t *testing.T) {
	cases := []struct {
		file string
		ids  []string
		want string
	}{
		{"SmartMoving-1.7.10-15.8.1-maka.jar", []string{"SmartMoving"}, "makamys/SmartMoving"},
		{"RenderPlayerAPIEnhancer-1.7.10-1.4.jar", nil, "makamys/RenderPlayerAPIEnhancer"},
		{"Other-1.0.jar", []string{"other"}, ""},
	}
	for _, c := range cases {
		m := &Mod{FileName: c.file, Jar: &jarinfo.Jar{ModIDs: c.ids}}
		if got := knownRepo(m); got != c.want {
			t.Errorf("%s: got %q want %q", c.file, got, c.want)
		}
	}
}

func TestReleaseJar(t *testing.T) {
	rel := &github.Release{Tag: "0.3.12", Assets: []github.Asset{
		{Name: "Thaumic-Utilities-.1.7.10.0.3.11-0.jar"},
		{Name: "Thaumic-Utilities-.1.7.10.0.3.12-0-sources.jar"},
		{Name: "Thaumic-Utilities-.1.7.10.0.3.12-0.jar"},
	}}
	if got := releaseJar(rel, "Thaumic-Utilities-.1.7.10.0.3.10-0.jar", "1.7.10"); got == nil || got.Name != "Thaumic-Utilities-.1.7.10.0.3.12-0.jar" {
		t.Errorf("got %v, want the jar of the release tag", got)
	}
	if got := releaseJar(rel, "Thaumic-Utilities-.1.7.10.0.3.11-0.jar", "1.7.10"); got == nil || got.Name != "Thaumic-Utilities-.1.7.10.0.3.11-0.jar" {
		t.Errorf("got %v, want the installed jar", got)
	}
	neo := &github.Release{Tag: "0.2.5", Assets: []github.Asset{
		{Name: "neodymium-1.7.10-0.2.5.jar"},
		{Name: "neodymium-1.7.10-0.2.5+nomixin.jar"},
	}}
	if got := releaseJar(neo, "neodymium-1.7.10-0.2.4+nomixin.jar", "1.7.10"); got == nil || got.Name != "neodymium-1.7.10-0.2.5+nomixin.jar" {
		t.Errorf("got %v, want the nomixin jar", got)
	}
}
