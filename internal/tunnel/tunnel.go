// Package tunnel exposes ssh.Client.Dial for arbitrary remote TCP destinations.
// We deliberately avoid spawning local listeners in-process; instead the proxy
// package creates fresh remote dials per request. This keeps lifecycle simple
// and avoids leaking ports if a client disconnects mid-stream.
package tunnel

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"
)

// Dial opens a new TCP channel through the SSH client to host:port on the
// remote side.
func Dial(ctx context.Context, cli *ssh.Client, host string, port int) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := cli.Dial("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
		ch <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.c, r.err
	}
}
