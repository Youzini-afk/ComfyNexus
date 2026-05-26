// Package proxy reverse-proxies HTTP and WebSocket traffic from the gateway to
// ComfyUI running on the active GPU instance, transported over an SSH-tunneled
// TCP connection.
package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
	"github.com/Youzini-afk/ComfyNexus/internal/tunnel"
	"golang.org/x/crypto/ssh"
)

// InstanceProvider returns the SSH target plus ComfyUI host/port for the
// currently active instance. The proxy calls this for every request so that
// switching the active instance takes effect immediately.
type InstanceProvider func(ctx context.Context) (target sshmgr.Target, comfyHost string, comfyPort int, err error)

// New builds an http.Handler that routes:
//   - GET /comfy/ws or any "Upgrade: websocket" request → WebSocket bridge
//   - everything else → HTTP reverse proxy
//
// stripPrefix is the URL prefix to strip before forwarding (typically "/comfy").
func New(mgr *sshmgr.Manager, provider InstanceProvider, stripPrefix string) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = "comfyui.local"
			r.URL.Path = strings.TrimPrefix(r.URL.Path, stripPrefix)
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			r.Host = "comfyui.local"
			r.Header.Set("X-Forwarded-Proto", "https")
			stripSensitiveProxyHeaders(r.Header)
		},
		Transport:     &sshTransport{mgr: mgr, provider: provider},
		FlushInterval: 100 * time.Millisecond,
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProxyUnavailable(w, r, err)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isWebSocketUpgrade(r) {
			handleWebSocket(w, r, mgr, provider, stripPrefix)
			return
		}
		rp.ServeHTTP(w, r)
	})
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	conn := r.Header.Get("Connection")
	return strings.Contains(strings.ToLower(conn), "upgrade")
}

// sshTransport is an http.RoundTripper that sends every request over a fresh
// TCP channel through the active SSH connection.
type sshTransport struct {
	mgr      *sshmgr.Manager
	provider InstanceProvider
}

func (t *sshTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tgt, host, port, err := t.provider(r.Context())
	if err != nil {
		return nil, err
	}
	cli, err := t.mgr.Get(r.Context(), tgt)
	if err != nil {
		return nil, err
	}
	conn, err := tunnel.Dial(r.Context(), cli, host, port)
	if err != nil {
		return nil, err
	}
	// One-shot HTTP/1.1 over the tunneled conn.
	if err := r.Write(conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, r)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp.Body = &readCloser{Reader: io.MultiReader(br, conn), Closer: conn}
	return resp, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

// handleWebSocket hijacks the client connection and dumb-pipes both directions.
// The browser ↔ gateway leg uses raw HTTP/1.1 hijack; the gateway ↔ ComfyUI leg
// uses the SSH tunnel.
func handleWebSocket(w http.ResponseWriter, r *http.Request, mgr *sshmgr.Manager, provider InstanceProvider, stripPrefix string) {
	tgt, host, port, err := provider(r.Context())
	if err != nil {
		writeProxyUnavailable(w, r, err)
		return
	}
	cli, err := mgr.Get(r.Context(), tgt)
	if err != nil {
		writeProxyUnavailable(w, r, err)
		return
	}
	upstream, err := tunnel.Dial(r.Context(), cli, host, port)
	if err != nil {
		writeProxyUnavailable(w, r, err)
		return
	}
	defer upstream.Close()

	// Forward the original WebSocket upgrade request to ComfyUI.
	upReq := r.Clone(r.Context())
	upReq.URL = &url.URL{Path: strings.TrimPrefix(r.URL.Path, stripPrefix), RawQuery: r.URL.RawQuery}
	if upReq.URL.Path == "" {
		upReq.URL.Path = "/"
	}
	upReq.Host = "comfyui.local"
	upReq.RequestURI = ""
	stripSensitiveProxyHeaders(upReq.Header)
	if err := upReq.Write(upstream); err != nil {
		writeProxyUnavailable(w, r, err)
		return
	}
	upstreamBuf := bufio.NewReader(upstream)
	upResp, err := http.ReadResponse(upstreamBuf, upReq)
	if err != nil {
		writeProxyUnavailable(w, r, err)
		return
	}
	upResp.Header.Del("Set-Cookie")
	if upResp.StatusCode != http.StatusSwitchingProtocols {
		copyResponseHeader(w.Header(), upResp.Header)
		w.WriteHeader(upResp.StatusCode)
		if upResp.Body != nil {
			defer upResp.Body.Close()
			_, _ = io.Copy(w, upResp.Body)
		}
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	clientConn, clientBuf, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()
	if err := upResp.Write(clientConn); err != nil {
		return
	}

	// Pipe both directions until one side closes.
	pipe(clientConn, upstream, clientBuf, upstreamBuf)
}

func copyResponseHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func stripSensitiveProxyHeaders(h http.Header) {
	for _, name := range []string{
		"Cookie",
		"Authorization",
		"Proxy-Authorization",
		"X-Requested-With",
		"X-Setup-Token",
		"ComfyNexus",
		"X-ComfyNexus",
		"X-Comfynexus",
	} {
		h.Del(name)
	}
	for name := range h {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "comfynexus") || strings.HasPrefix(lower, "x-comfynexus") {
			h.Del(name)
		}
	}
}

func writeProxyUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusServiceUnavailable
	code := errs.CodeInstanceUnreach
	msg := "ComfyUI is unavailable"
	var appErr *errs.Error
	if errors.As(err, &appErr) {
		code = appErr.Code
		if appErr.Message != "" {
			msg = appErr.Message
		}
	} else if err != nil && err.Error() != "" {
		msg = "ComfyUI is unavailable: " + err.Error()
	}
	if acceptsHTML(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "<!doctype html><html><head><title>ComfyUI unavailable</title></head><body><h1>ComfyUI unavailable</h1><p>%s</p></body></html>", htmlEscape(msg))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

func acceptsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func pipe(client net.Conn, upstream net.Conn, clientBuf *bufio.ReadWriter, upstreamBuf *bufio.Reader) {
	done := make(chan struct{}, 2)
	go func() {
		// upstream → client (response + frames)
		if upstreamBuf != nil && upstreamBuf.Buffered() > 0 {
			_, _ = io.CopyN(client, upstreamBuf, int64(upstreamBuf.Buffered()))
		}
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	go func() {
		// client → upstream (drain hijacked buffer first, then conn)
		if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
			_, _ = io.CopyN(upstream, clientBuf, int64(clientBuf.Reader.Buffered()))
		}
		_, _ = io.Copy(upstream, client)
		done <- struct{}{}
	}()
	<-done
}

// Helper: cast away to silence unused import warnings if buildtags hide ssh.
var _ = (*ssh.Client)(nil)
var _ fmt.Stringer
