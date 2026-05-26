// Package sshmgr maintains long-lived SSH connections to GPU instances with
// automatic reconnect, keep-alive, and multiplexed access for tunnels, SFTP,
// and exec channels.
package sshmgr

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target describes how to dial an instance.
type Target struct {
	ID                       int64
	Name                     string
	Host                     string
	Port                     int
	User                     string
	PrivateKeyPEM            []byte // either PEM bytes (inline) or empty if KeyPath is set
	KeyPath                  string // mounted file path (when inline is empty)
	Passphrase               []byte // optional passphrase for encrypted keys
	HostFingerprint          string // optional pinned SHA256 host key fingerprint ("SHA256:...")
	InsecureSkipHostKeyCheck bool   // explicit opt-in for legacy trust-any host behavior
}

// Manager owns one Conn per instance ID.
type Manager struct {
	mu    sync.Mutex
	conns map[int64]*Conn
}

func New() *Manager { return &Manager{conns: map[int64]*Conn{}} }

// Get returns a live SSH client for the target, dialing or reconnecting as
// needed. Callers must not retain the *ssh.Client across reconnects; instead
// re-Get for each operation if the previous one returned an error.
func (m *Manager) Get(ctx context.Context, t Target) (*ssh.Client, error) {
	m.mu.Lock()
	c, ok := m.conns[t.ID]
	if !ok {
		c = &Conn{target: t}
		m.conns[t.ID] = c
	}
	m.mu.Unlock()
	return c.dial(ctx)
}

// CloseInstance forcefully drops the cached connection for an instance.
// Used when an instance config is updated.
func (m *Manager) CloseInstance(id int64) {
	m.mu.Lock()
	c, ok := m.conns[id]
	delete(m.conns, id)
	m.mu.Unlock()
	if ok {
		c.close()
	}
}

// CloseAll shuts down all cached connections (server stop).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	conns := m.conns
	m.conns = map[int64]*Conn{}
	m.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

// Conn wraps a single ssh.Client with re-dial logic.
type Conn struct {
	mu     sync.Mutex
	target Target
	client *ssh.Client
}

func (c *Conn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
}

func (c *Conn) dial(ctx context.Context) (*ssh.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Reuse if alive: a quick keepalive request.
	if c.client != nil {
		_, _, err := c.client.SendRequest("keepalive@comfynexus", true, nil)
		if err == nil {
			return c.client, nil
		}
		_ = c.client.Close()
		c.client = nil
	}

	cfg, err := buildClientConfig(c.target)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(c.target.Host, fmt.Sprintf("%d", c.target.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	netConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	cli := ssh.NewClient(sshConn, chans, reqs)
	c.client = cli
	go c.keepalive(cli)
	return cli, nil
}

func (c *Conn) keepalive(cli *ssh.Client) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for range t.C {
		c.mu.Lock()
		alive := c.client == cli
		c.mu.Unlock()
		if !alive {
			return
		}
		_, _, err := cli.SendRequest("keepalive@comfynexus", true, nil)
		if err != nil {
			c.mu.Lock()
			if c.client == cli {
				_ = c.client.Close()
				c.client = nil
			}
			c.mu.Unlock()
			return
		}
	}
}

func buildClientConfig(t Target) (*ssh.ClientConfig, error) {
	keyBytes := t.PrivateKeyPEM
	if len(keyBytes) == 0 && t.KeyPath != "" {
		b, err := os.ReadFile(t.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("read key file: %w", err)
		}
		keyBytes = b
	}
	if len(keyBytes) == 0 {
		return nil, errors.New("no private key provided")
	}
	var signer ssh.Signer
	var err error
	if len(t.Passphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, t.Passphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(keyBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	hostKeyCb := ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if t.HostFingerprint == "" {
			if t.InsecureSkipHostKeyCheck {
				return nil
			}
			got := "SHA256:" + base64NoPad(sha256.Sum256(key.Marshal()))
			return fmt.Errorf("ssh host key fingerprint required for %s (server presented %s); set the instance hostFingerprint or explicitly set COMFYNEXUS_INSECURE_SKIP_HOST_KEY_CHECK=true for development only", hostname, got)
		}
		got := "SHA256:" + base64NoPad(sha256.Sum256(key.Marshal()))
		if got != t.HostFingerprint {
			return fmt.Errorf("host key mismatch: got %s want %s", got, t.HostFingerprint)
		}
		return nil
	})

	return &ssh.ClientConfig{
		User:            t.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCb,
		Timeout:         15 * time.Second,
	}, nil
}

func base64NoPad(sum [32]byte) string {
	return base64.RawStdEncoding.EncodeToString(sum[:])
}
