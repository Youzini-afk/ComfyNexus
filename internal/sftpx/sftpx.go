// Package sftpx contains small SFTP helpers shared by HTTP handlers.
package sftpx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Entry is the wire format for file-browser entries.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Type    string    `json:"type"` // "file" or "dir"
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// NewClient opens an SFTP client over an existing SSH connection.
func NewClient(sshClient *ssh.Client) (*sftp.Client, error) {
	return sftp.NewClient(sshClient)
}

// CleanPath accepts absolute-like Comfy paths (for example /models/loras),
// rejects traversal/control characters, and returns a stable cleaned path.
func CleanPath(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if strings.Contains(p, "\x00") {
		return "", errors.New("path contains NUL")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return "", errors.New("path contains control character")
		}
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == ".." {
			return "", errors.New("path traversal is not allowed")
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/", nil
	}
	return cleaned, nil
}

// List returns sorted directory entries in the SFTP server's native order.
func List(c *sftp.Client, dir string) ([]Entry, error) {
	infos, err := c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(infos))
	for _, info := range infos {
		typ := "file"
		if info.IsDir() {
			typ = "dir"
		}
		out = append(out, Entry{
			Name:    info.Name(),
			Path:    path.Join(dir, info.Name()),
			Type:    typ,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return out, nil
}

// Remove deletes a file or directory tree.
func Remove(c *sftp.Client, p string) error {
	info, err := c.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return c.RemoveAll(p)
	}
	return c.Remove(p)
}

// OpenAppend opens p for sequential upload appends. Chunk zero truncates any
// stale temp file so retries of newly-created jobs start cleanly.
func OpenAppend(c *sftp.Client, p string, chunk int) (io.WriteCloser, error) {
	flags := os.O_CREATE | os.O_WRONLY
	if chunk == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	return c.OpenFile(p, flags)
}

// EnsureParent creates the parent directory for p.
func EnsureParent(c *sftp.Client, p string) error {
	parent := path.Dir(p)
	if parent == "." || parent == "/" {
		return nil
	}
	return c.MkdirAll(parent)
}

// StatSize returns the remote file size.
func StatSize(c *sftp.Client, p string) (int64, error) {
	info, err := c.Stat(p)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", p)
	}
	return info.Size(), nil
}
