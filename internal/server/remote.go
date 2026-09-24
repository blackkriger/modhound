package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/resolve"
)

type Target struct {
	User string
	Host string
	Port int
	Path string
}

var scpTarget = regexp.MustCompile(`^([^@\s/\\]+)@([^:\s/\\]+):(.*)$`)

func IsRemote(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "sftp://") || scpTarget.MatchString(s)
}

func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "sftp://") {
		u, err := url.Parse(s)
		if err != nil || u.User == nil || u.Hostname() == "" {
			return Target{}, fmt.Errorf("%q is not a server address, use user@host:/path", s)
		}
		t := Target{User: u.User.Username(), Host: u.Hostname(), Port: 22, Path: u.Path}
		if p := u.Port(); p != "" {
			t.Port, _ = strconv.Atoi(p)
		}
		return t, nil
	}
	m := scpTarget.FindStringSubmatch(s)
	if m == nil {
		return Target{}, fmt.Errorf("%q is not a server address, use user@host:/path", s)
	}
	return Target{User: m[1], Host: m[2], Port: 22, Path: m[3]}, nil
}

func (t Target) String() string {
	if t.Port != 22 {
		return fmt.Sprintf("sftp://%s@%s%s", t.User, net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), path.Clean("/"+t.Path))
	}
	return t.User + "@" + t.Host + ":" + t.Path
}

func (t Target) modsDir() string {
	p := t.Path
	if p == "" {
		p = "."
	}
	if path.Base(p) == "mods" {
		return p
	}
	return path.Join(p, "mods")
}

func (t Target) backupDir(session string) string {
	return path.Join(path.Dir(t.modsDir()), "modhound-backups", session)
}

var KeyFor func(target string) string

func DefaultKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

type conn struct {
	ssh  *ssh.Client
	sftp *sftp.Client
}

var (
	poolMu sync.Mutex
	pool   = map[string]*conn{}
)

func dial(t Target, keyPath string) (*conn, error) {
	if keyPath == "" {
		return nil, errors.New("choose an SSH key")
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the SSH key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return nil, errors.New("SSH keys with a passphrase are not supported")
	}
	if err != nil {
		return nil, fmt.Errorf("%s is not an OpenSSH private key: %w", filepath.Base(keyPath), err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	hostKeys, err := knownhosts.New(filepath.Join(home, ".ssh", "known_hosts"))
	if err != nil {
		return nil, fmt.Errorf("cannot read ~/.ssh/known_hosts: %w", err)
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    []ssh.AuthMethod{ssh.PublicKeys(signer)},
		Timeout: 15 * time.Second,
		HostKeyCallback: func(host string, remote net.Addr, key ssh.PublicKey) error {
			err := hostKeys(host, remote, key)
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) {
				if len(keyErr.Want) == 0 {
					return fmt.Errorf("%s is not in ~/.ssh/known_hosts", t.Host)
				}
				return fmt.Errorf("the host key of %s does not match ~/.ssh/known_hosts", t.Host)
			}
			return err
		},
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", t.Host, err)
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("%s does not allow SFTP: %w", t.Host, err)
	}
	logx.Printf("sftp: connected to %s as %s", addr, t.User)
	return &conn{ssh: client, sftp: sc}, nil
}

func withSFTP(t Target, fn func(sc *sftp.Client) error) error {
	key := ""
	if KeyFor != nil {
		key = KeyFor(t.String())
	}
	id := t.String() + "|" + key
	for attempt := 0; attempt < 2; attempt++ {
		poolMu.Lock()
		c := pool[id]
		poolMu.Unlock()
		if c == nil {
			var err error
			if c, err = dial(t, key); err != nil {
				return err
			}
			poolMu.Lock()
			pool[id] = c
			poolMu.Unlock()
		}
		err := fn(c.sftp)
		if err == nil {
			return nil
		}
		if _, alive := c.sftp.Getwd(); alive == nil || attempt == 1 {
			return err
		}
		poolMu.Lock()
		delete(pool, id)
		poolMu.Unlock()
		c.sftp.Close()
		c.ssh.Close()
	}
	return nil
}

func CheckRemote(target, keyPath string) error {
	t, err := ParseTarget(target)
	if err != nil {
		return err
	}
	c, err := dial(t, keyPath)
	if err != nil {
		return err
	}
	defer c.ssh.Close()
	defer c.sftp.Close()
	info, err := c.sftp.Stat(t.modsDir())
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no mods folder in %s", target)
	}
	return nil
}

func compareRemote(client *resolve.Pack, target string) (*Diff, error) {
	t, err := ParseTarget(target)
	if err != nil {
		return nil, err
	}
	var files []string
	err = withSFTP(t, func(sc *sftp.Client) error {
		files = nil
		mods := t.modsDir()
		entries, err := sc.ReadDir(mods)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", mods, err)
		}
		for _, e := range entries {
			if e.IsDir() && isVersionDir(e.Name()) {
				sub, err := sc.ReadDir(path.Join(mods, e.Name()))
				if err != nil {
					return err
				}
				for _, s := range sub {
					if !s.IsDir() && isJar(s.Name()) {
						files = append(files, path.Join(mods, e.Name(), s.Name()))
					}
				}
			} else if !e.IsDir() && isJar(e.Name()) {
				files = append(files, path.Join(mods, e.Name()))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	byStem := map[string]*resolve.Mod{}
	for _, m := range client.Mods {
		if stem := resolve.Stem(m.FileName); stem != "" {
			byStem[stem] = m
		}
	}
	diff := &Diff{Root: target, Mods: len(files), remote: true}
	for _, f := range files {
		name := path.Base(f)
		m := byStem[resolve.Stem(name)]
		if m == nil || strings.EqualFold(name, m.FileName) || !resolve.OlderVersion(name, m.FileName, client.MCVersion) {
			continue
		}
		diff.Items = append(diff.Items, Item{Name: m.Name, ClientFile: m.FileName, ServerFile: name, clientPath: m.Path, serverPath: f, key: m.Key})
	}
	logx.Printf("server %s: %d mods, %d behind the client", target, diff.Mods, len(diff.Items))
	return diff, nil
}

func isJar(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".zip")
}

func syncRemote(diff *Diff, session *backup.Session, mcVersion string) ([]Result, error) {
	t, err := ParseTarget(diff.Root)
	if err != nil {
		return nil, err
	}
	var results []Result
	err = withSFTP(t, func(sc *sftp.Client) error {
		results = nil
		for _, it := range diff.Items {
			res := Result{Name: it.Name, File: it.ClientFile}
			if err := syncOneRemote(sc, t, session, it, mcVersion); err != nil {
				res.Error = err.Error()
			} else {
				res.OK = true
			}
			logx.Printf("server sync %s: %s -> %s, ok=%v %s", it.Name, it.ServerFile, it.ClientFile, res.OK, res.Error)
			results = append(results, res)
		}
		return nil
	})
	return results, err
}

func syncOneRemote(sc *sftp.Client, t Target, session *backup.Session, it Item, mcVersion string) error {
	dir := path.Dir(it.serverPath)
	dest := path.Join(dir, it.ClientFile)
	if dest != it.serverPath {
		if _, err := sc.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists on the server", it.ClientFile)
		}
	}
	part := path.Join(dir, ".modhound-sync.part")
	if err := upload(sc, it.clientPath, part); err != nil {
		sc.Remove(part)
		return fmt.Errorf("cannot upload %s: %w", it.ClientFile, err)
	}
	backups := t.backupDir(session.ID)
	if err := sc.MkdirAll(backups); err != nil {
		sc.Remove(part)
		return fmt.Errorf("cannot create %s: %w", backups, err)
	}
	stored := path.Join(backups, it.ServerFile)
	for n := 1; ; n++ {
		if _, err := sc.Stat(stored); err != nil {
			break
		}
		stored = path.Join(backups, fmt.Sprintf("%d-%s", n, it.ServerFile))
	}
	if err := sc.Rename(it.serverPath, stored); err != nil {
		sc.Remove(part)
		return fmt.Errorf("cannot move the old server file: %w", err)
	}
	item := backup.Item{Key: KeyPrefix + it.key, Name: it.Name, Dir: dir, OldFile: it.ServerFile, NewFile: it.ClientFile, Stored: stored, From: resolve.FileVersion(it.ServerFile, mcVersion), To: resolve.FileVersion(it.ClientFile, mcVersion), Remote: t.String()}
	if err := sc.Rename(part, dest); err != nil {
		if backErr := sc.Rename(stored, it.serverPath); backErr != nil {
			session.Add(item)
			return fmt.Errorf("cannot place the new file, the old one is kept in the backup: %w", err)
		}
		sc.Remove(part)
		return fmt.Errorf("cannot place the new file: %w", err)
	}
	session.Add(item)
	return nil
}

func upload(sc *sftp.Client, local, remote string) error {
	in, err := os.Open(local)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := sc.Create(remote)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

type remoteStore struct{}

func (remoteStore) Restore(it backup.Item) error {
	t, err := ParseTarget(it.Remote)
	if err != nil {
		return err
	}
	return withSFTP(t, func(sc *sftp.Client) error {
		if _, err := sc.Stat(it.Stored); err != nil {
			return errors.New("the saved copy is missing")
		}
		current := path.Join(it.Dir, it.NewFile)
		original := path.Join(it.Dir, it.OldFile)
		if current != original {
			if _, err := sc.Stat(original); err == nil {
				return fmt.Errorf("%s already exists", it.OldFile)
			}
		}
		aside := current + ".modhound-undo"
		if _, err := sc.Stat(current); err == nil {
			if err := sc.Rename(current, aside); err != nil {
				return fmt.Errorf("cannot remove the new file: %w", err)
			}
		}
		if err := sc.Rename(it.Stored, original); err != nil {
			sc.Rename(aside, current)
			return fmt.Errorf("cannot put the old file back: %w", err)
		}
		sc.Remove(aside)
		sc.RemoveDirectory(path.Dir(it.Stored))
		return nil
	})
}

func (remoteStore) Remove(it backup.Item) error {
	t, err := ParseTarget(it.Remote)
	if err != nil {
		return err
	}
	return withSFTP(t, func(sc *sftp.Client) error {
		err := sc.Remove(it.Stored)
		sc.RemoveDirectory(path.Dir(it.Stored))
		return err
	})
}

func init() {
	backup.Remote = remoteStore{}
}
