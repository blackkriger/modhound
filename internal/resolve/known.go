package resolve

import (
	"regexp"
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
	jars := forMinecraft(rel.Jars(Stem, Stem(installed)), mcVersion)
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

func forMinecraft(jars []github.Asset, mcVersion string) []github.Asset {
	var own []github.Asset
	for _, a := range jars {
		if mcVersion != "" && strings.Contains(a.Name, mcVersion) {
			own = append(own, a)
		}
	}
	if len(own) > 0 {
		return own
	}
	var neutral []github.Asset
	for _, a := range jars {
		if !forOtherMinecraft(a.Name, mcVersion) {
			neutral = append(neutral, a)
		}
	}
	return neutral
}

var minecraftToken = regexp.MustCompile(`^(mc)?1\.\d+(\.\d+)?$`)

func forOtherMinecraft(fileName, mcVersion string) bool {
	tokens := versionSplit.Split(strings.ToLower(strings.TrimSuffix(fileName, ".jar")), -1)
	numbered := 0
	for _, t := range tokens {
		if strings.ContainsAny(t, "0123456789") {
			numbered++
		}
	}
	for _, t := range tokens {
		m := minecraftToken.FindStringSubmatch(t)
		if m == nil || strings.TrimPrefix(t, "mc") == mcVersion || strings.HasPrefix(mcVersion, strings.TrimPrefix(t, "mc")+".") {
			continue
		}
		if m[1] != "" || numbered > 1 {
			return true
		}
	}
	return false
}

func renamedGitHubJar(releases []github.Release, installed, mcVersion string) (*github.Release, *github.Asset) {
	var best *github.Release
	var asset *github.Asset
	for i := range releases {
		rel := &releases[i]
		if rel.Draft || rel.Prerelease || best != nil && !rel.Published.After(best.Published) {
			continue
		}
		for _, a := range forMinecraft(rel.Jars(Stem, ""), mcVersion) {
			if sharesStem(a.Name, installed) && !isVariantOf(a.Name, installed) {
				best, asset = rel, &a
				break
			}
		}
	}
	if asset == nil || sameVersion(asset.Name, installed, mcVersion) || OlderVersion(asset.Name, installed, mcVersion) {
		return nil, nil
	}
	return best, asset
}
