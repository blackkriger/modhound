package jarinfo

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

type ModInfo struct {
	ModID       string     `json:"modid"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Version     string     `json:"version"`
	MCVersion   string     `json:"mcversion"`
	URL         string     `json:"url"`
	AuthorList  StringList `json:"authorList"`
	Authors     StringList `json:"authors"`
	LogoFile    string     `json:"logoFile"`
	Required    StringList `json:"requiredMods"`
}

type StringList []string

func (l *StringList) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		if one = strings.TrimSpace(one); one != "" {
			*l = StringList{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		*l = nil
		return nil
	}
	*l = many
	return nil
}

type Jar struct {
	Size       int64
	SHA1       string
	SHA512     string
	Murmur2    uint32
	Info       *ModInfo
	Logo       []byte
	LogoType   string
	ModIDs     []string
	Requires   []string
	ClassMajor int
	Valid      bool
}

const maxLogoSize = 1 << 20

func Read(p string) (*Jar, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return parse(data, false), nil
}

func ReadFull(p string) (*Jar, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return parse(data, true), nil
}

func parse(data []byte, withSHA512 bool) *Jar {
	s1 := sha1.Sum(data)
	j := &Jar{
		Size:    int64(len(data)),
		SHA1:    hex.EncodeToString(s1[:]),
		Murmur2: Murmur2(data),
	}
	if withSHA512 {
		s512 := sha512.Sum512(data)
		j.SHA512 = hex.EncodeToString(s512[:])
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return j
	}
	j.Valid = true
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
		if strings.HasSuffix(f.Name, ".class") && !strings.HasPrefix(f.Name, "META-INF/") && !strings.HasSuffix(f.Name, "module-info.class") {
			if m := classMajor(f); m > j.ClassMajor {
				j.ClassMajor = m
			}
		}
	}
	if f, ok := files["mcmod.info"]; ok {
		list := parseModInfo(readAll(f, 1<<20))
		if len(list) > 0 {
			j.Info = list[0]
		}
		for _, mi := range list {
			if mi.ModID != "" {
				j.ModIDs = append(j.ModIDs, mi.ModID)
			}
			j.Requires = append(j.Requires, mi.Required...)
		}
	}
	if j.Info != nil && j.Info.LogoFile != "" {
		j.Logo, j.LogoType = readLogo(files, j.Info.LogoFile)
	}
	return j
}

func (j *Jar) Authors() []string {
	if j.Info == nil {
		return nil
	}
	if len(j.Info.AuthorList) > 0 {
		return j.Info.AuthorList
	}
	return j.Info.Authors
}

func classMajor(f *zip.File) int {
	rc, err := f.Open()
	if err != nil {
		return 0
	}
	defer rc.Close()
	var h [8]byte
	if _, err := io.ReadFull(rc, h[:]); err != nil {
		return 0
	}
	if h[0] != 0xCA || h[1] != 0xFE || h[2] != 0xBA || h[3] != 0xBE {
		return 0
	}
	return int(h[6])<<8 | int(h[7])
}

func readAll(f *zip.File, limit int64) []byte {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, limit))
	return b
}

var trailingComma = regexp.MustCompile(`,\s*([\]}])`)

func parseModInfo(raw []byte) []*ModInfo {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if len(raw) == 0 {
		return nil
	}
	if list := decodeModInfo(raw); list != nil {
		return list
	}
	cleaned := bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, raw)
	cleaned = trailingComma.ReplaceAll(cleaned, []byte("$1"))
	return decodeModInfo(cleaned)
}

func decodeModInfo(raw []byte) []*ModInfo {
	var list []ModInfo
	if json.Unmarshal(raw, &list) != nil || len(list) == 0 {
		var wrapped struct {
			ModList []ModInfo `json:"modList"`
		}
		if json.Unmarshal(raw, &wrapped) != nil || len(wrapped.ModList) == 0 {
			return nil
		}
		list = wrapped.ModList
	}
	out := make([]*ModInfo, len(list))
	for i := range list {
		out[i] = normalize(&list[i])
	}
	return out
}

func normalize(mi *ModInfo) *ModInfo {
	for _, s := range []*string{&mi.Version, &mi.MCVersion} {
		if strings.Contains(*s, "${") || strings.Contains(*s, "@") {
			*s = ""
		}
	}
	return mi
}

func readLogo(files map[string]*zip.File, name string) ([]byte, string) {
	name = strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
	f, ok := files[name]
	if !ok || f.UncompressedSize64 > maxLogoSize {
		return nil, ""
	}
	mime := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif"}[strings.ToLower(path.Ext(name))]
	if mime == "" {
		return nil, ""
	}
	b := readAll(f, maxLogoSize)
	if len(b) == 0 {
		return nil, ""
	}
	return b, mime
}

func Murmur2(data []byte) uint32 {
	buf := make([]byte, 0, len(data))
	for _, b := range data {
		if b != 9 && b != 10 && b != 13 && b != 32 {
			buf = append(buf, b)
		}
	}
	const m = 0x5bd1e995
	h := uint32(1) ^ uint32(len(buf))
	i := 0
	for ; i+4 <= len(buf); i += 4 {
		k := uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24
		k *= m
		k ^= k >> 24
		k *= m
		h *= m
		h ^= k
	}
	switch len(buf) - i {
	case 3:
		h ^= uint32(buf[i+2]) << 16
		fallthrough
	case 2:
		h ^= uint32(buf[i+1]) << 8
		fallthrough
	case 1:
		h ^= uint32(buf[i])
		h *= m
	}
	h ^= h >> 13
	h *= m
	h ^= h >> 15
	return h
}
