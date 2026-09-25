package fsx

import (
	"crypto/ed25519"
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

	"github.com/blackkriger/modhound/internal/logx"
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

func (t Target) Root() string {
	if t.Path == "" {
		return "."
	}
	return t.Path
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

const (
	dialTimeout   = 15 * time.Second
	keepaliveEach = 15 * time.Second
)

type conn struct {
	ssh  *ssh.Client
	sftp *sftp.Client
	done chan struct{}
}

func (c *conn) dead() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

var (
	poolMu sync.Mutex
	pool   = map[string]*conn{}
)

func knownAlgorithms(hostKeys ssh.HostKeyCallback, addr string) []string {
	remote, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil
	}
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil
	}
	probe, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil
	}
	var keyErr *knownhosts.KeyError
	if !errors.As(hostKeys(addr, remote, probe), &keyErr) {
		return nil
	}
	var algos []string
	for _, k := range keyErr.Want {
		if k.Key.Type() == ssh.KeyAlgoRSA {
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		} else {
			algos = append(algos, k.Key.Type())
		}
	}
	return algos
}

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
		User:              t.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyAlgorithms: knownAlgorithms(hostKeys, addr),
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
	raw, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", t.Host, err)
	}
	raw.SetDeadline(time.Now().Add(dialTimeout))
	sc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		raw.Close()
		return nil, fmt.Errorf("cannot connect to %s: %w", t.Host, err)
	}
	raw.SetDeadline(time.Time{})
	client := ssh.NewClient(sc, chans, reqs)
	files, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("%s does not allow SFTP: %w", t.Host, err)
	}
	c := &conn{ssh: client, sftp: files, done: make(chan struct{})}
	go func() {
		client.Wait()
		close(c.done)
	}()
	go keepalive(c)
	logx.Printf("sftp: connected to %s as %s", addr, t.User)
	return c, nil
}

func keepalive(c *conn) {
	tick := time.NewTicker(keepaliveEach)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tick.C:
		}
		reply := make(chan error, 1)
		go func() {
			_, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		select {
		case err := <-reply:
			if err == nil {
				continue
			}
		case <-time.After(keepaliveEach):
		}
		logx.Printf("sftp: connection lost")
		c.sftp.Close()
		c.ssh.Close()
		return
	}
}

func connect(t Target) (*conn, error) {
	key := ""
	if KeyFor != nil {
		key = KeyFor(t.String())
	}
	id := t.String() + "|" + key
	poolMu.Lock()
	defer poolMu.Unlock()
	if c := pool[id]; c != nil && !c.dead() {
		return c, nil
	}
	c, err := dial(t, key)
	if err != nil {
		return nil, err
	}
	pool[id] = c
	return c, nil
}

func withSFTP(t Target, retry bool, fn func(sc *sftp.Client) error) error {
	c, err := connect(t)
	if err != nil {
		return err
	}
	err = fn(c.sftp)
	if err == nil || !retry || !c.dead() {
		return err
	}
	if c, err = connect(t); err != nil {
		return err
	}
	return fn(c.sftp)
}

func Check(target, keyPath string) error {
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
	f := &sftpFS{t: t, fixed: c}
	if _, err := ModsDir(f, t.Root()); err != nil {
		return fmt.Errorf("no mods folder in %s", target)
	}
	return nil
}

type sftpFS struct {
	t     Target
	fixed *conn
}

func (f *sftpFS) do(retry bool, fn func(sc *sftp.Client) error) error {
	if f.fixed != nil {
		return fn(f.fixed.sftp)
	}
	return withSFTP(f.t, retry, fn)
}

func (f *sftpFS) ReadDir(dir string) ([]Entry, error) {
	var out []Entry
	err := f.do(true, func(sc *sftp.Client) error {
		infos, err := sc.ReadDir(dir)
		out = out[:0]
		for _, i := range infos {
			out = append(out, Entry{Name: i.Name(), Dir: i.IsDir()})
		}
		return err
	})
	return out, err
}

func (f *sftpFS) IsDir(p string) bool {
	dir := false
	f.do(true, func(sc *sftp.Client) error {
		info, err := sc.Stat(p)
		dir = err == nil && info.IsDir()
		return err
	})
	return dir
}

func (f *sftpFS) Exists(p string) bool {
	return f.do(true, func(sc *sftp.Client) error {
		_, err := sc.Stat(p)
		return err
	}) == nil
}

func (f *sftpFS) Rename(from, to string) error {
	return f.do(false, func(sc *sftp.Client) error { return sc.Rename(from, to) })
}

func (f *sftpFS) Remove(p string) error {
	return f.do(false, func(sc *sftp.Client) error { return sc.Remove(p) })
}

func (f *sftpFS) RemoveEmptyDir(p string) {
	f.do(false, func(sc *sftp.Client) error { return sc.RemoveDirectory(p) })
}

func (f *sftpFS) MkdirAll(p string) error {
	return f.do(false, func(sc *sftp.Client) error { return sc.MkdirAll(p) })
}

func (f *sftpFS) Upload(local, p string) error {
	return f.do(false, func(sc *sftp.Client) error {
		in, err := os.Open(local)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := sc.Create(p)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func (f *sftpFS) Join(elem ...string) string { return path.Join(elem...) }

func (f *sftpFS) Dir(p string) string { return path.Dir(p) }

func (f *sftpFS) Base(p string) string { return path.Base(p) }

func (f *sftpFS) Remote() string { return f.t.String() }
