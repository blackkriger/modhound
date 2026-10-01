package resolve

import (
	"strings"

	"github.com/blackkriger/modhound/internal/github"
)

const SourceGitHub = "github"

type knownMod struct {
	modID string
	stem  string
	repo  string
}

var knownRepos = []knownMod{
	{modID: "hbm", repo: "HbmMods/Hbm-s-Nuclear-Tech-GIT"},
	{modID: "aether_legacy", repo: "AetherLegacyPro/AetherLegacyDeparture"},
	{modID: "neodymium", repo: "makamys/Neodymium"},
	{modID: "smartmoving", repo: "makamys/SmartMoving"},
	{stem: "renderplayerapienhancer", repo: "makamys/RenderPlayerAPIEnhancer"},
	{modID: "thaumicalchemy", repo: "KryptonCaptain/ThaumicAlchemy"},
	{modID: "terrata", repo: "KryptonCaptain/ThaumErrata"},
	{modID: "thutilities", repo: "KryptonCaptain/ThaumicUtilities"},
}

func knownRepo(m *Mod) string {
	if m.Jar == nil {
		return ""
	}
	stem := Stem(m.FileName)
	for _, k := range knownRepos {
		if k.stem != "" && k.stem == stem {
			return k.repo
		}
		for _, id := range m.Jar.ModIDs {
			if k.modID != "" && strings.EqualFold(id, k.modID) {
				return k.repo
			}
		}
	}
	return ""
}

func releaseJar(rel *github.Release, installed, mcVersion string) *github.Asset {
	jars := rel.Jars(Stem, Stem(installed))
	var best *github.Asset
	tag := strings.TrimPrefix(strings.ToLower(rel.Tag), "v")
	for i := range jars {
		a := &jars[i]
		switch {
		case strings.EqualFold(a.Name, installed):
			return a
		case best == nil:
			best = a
		case strings.Contains(strings.ToLower(a.Name), tag) != strings.Contains(strings.ToLower(best.Name), tag):
			if strings.Contains(strings.ToLower(a.Name), tag) {
				best = a
			}
		case OlderVersion(best.Name, a.Name, mcVersion):
			best = a
		}
	}
	return best
}
