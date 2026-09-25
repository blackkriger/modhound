package fsx

import "testing"

func TestParseTarget(t *testing.T) {
	for _, local := range []string{`C:\Games\server`, `D:/server/mods`, `server`, ``} {
		if IsRemote(local) {
			t.Fatalf("%q is local", local)
		}
	}
	cases := map[string]Target{
		"root@37.221.192.94:/root/server":      {User: "root", Host: "37.221.192.94", Port: 22, Path: "/root/server"},
		"mc@example.org:server":                {User: "mc", Host: "example.org", Port: 22, Path: "server"},
		"sftp://mc@example.org:2022/home/mods": {User: "mc", Host: "example.org", Port: 2022, Path: "/home/mods"},
	}
	for in, want := range cases {
		if !IsRemote(in) {
			t.Fatalf("%q is remote", in)
		}
		got, err := ParseTarget(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %+v %v, want %+v", in, got, err, want)
		}
		if again, _ := ParseTarget(got.String()); again != want {
			t.Fatalf("%q does not survive String(): %+v", in, again)
		}
	}
	if got := (Target{}).Root(); got != "." {
		t.Fatalf("Root = %q", got)
	}
}
