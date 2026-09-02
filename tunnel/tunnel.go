// Package tunnel carries iris sessions between machines over tailcat:
// WireGuard end to end, DERP for rendezvous and fallback.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/logger"
	"tailscale.com/wgengine/filter"
)

// Port is the port a relay is served on inside the tunnel.
const Port = 7433

// Token is the pairing token: the tailcat connection blob plus the
// session it admits. Possession of the string is membership.
type Token struct {
	Blob tailcat.ConnBlob
	UID  string
	Key  string
}

func (t Token) String() string {
	return string(t.Blob) + "." + t.UID + "." + t.Key
}

// Parse decodes a pairing token, checking the blob parses.
func Parse(s string) (Token, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "tc") || parts[1] == "" || parts[2] == "" {
		return Token{}, errors.New("malformed pairing token")
	}
	t := Token{Blob: tailcat.ConnBlob(parts[0]), UID: parts[1], Key: parts[2]}
	if _, err := tailcat.ParseConnBlob(t.Blob); err != nil {
		return Token{}, fmt.Errorf("pairing token: %w", err)
	}
	return t, nil
}

// Listener is the host side of a tunnel: a net.Listener whose
// connections arrive from peers dialing Port.
type Listener struct {
	srv   *tailcat.Server
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

// Listen opens the host side. derp, if non-empty, names self-hosted
// DERP servers to use instead of the default map.
func Listen(derp []string, logf logger.Logf) (*Listener, error) {
	l := &Listener{conns: make(chan net.Conn), done: make(chan struct{})}
	l.srv = &tailcat.Server{
		Logf:           logf,
		ServedTCPPorts: []filter.PortRange{{First: Port, Last: Port}},
		OnTCP: func(port uint16) func(net.Conn) {
			if port != Port {
				return nil
			}
			return l.handle
		},
	}
	if len(derp) > 0 {
		l.srv.Region = &tailcfg.DERPRegion{}
		for _, host := range derp {
			l.srv.Region.Nodes = append(l.srv.Region.Nodes, &tailcfg.DERPNode{HostName: host})
		}
	}
	if err := l.srv.Start(); err != nil {
		return nil, err
	}
	return l, nil
}

// handle runs on tailcat's per-connection goroutine; the conn outlives it.
func (l *Listener) handle(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		c.Close()
	}
}

// Blob is the connection token peers use to reach this listener.
func (l *Listener) Blob() tailcat.ConnBlob { return l.srv.ConnBlob() }

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *Listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.srv.Close()
}

func (l *Listener) Addr() net.Addr {
	return &net.TCPAddr{IP: l.srv.Addr().AsSlice(), Port: Port}
}

// Peer is the client side of a tunnel to one host.
type Peer struct {
	cl *tailcat.Client
}

// Dial brings up a tunnel to the host named by blob and confirms it
// answers. It fails when the host is unreachable within ctx.
func Dial(ctx context.Context, blob tailcat.ConnBlob, logf logger.Logf) (*Peer, error) {
	p := &Peer{cl: &tailcat.Client{Server: blob, Logf: logf}}
	if _, err := p.cl.Ping(ctx); err != nil {
		p.cl.Close()
		return nil, err
	}
	return p, nil
}

// Forward serves ln, piping each accepted connection to Port on the
// host, until ctx is done or ln fails.
func (p *Peer) Forward(ctx context.Context, ln net.Listener, logf logger.Logf) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func() {
			rc, err := p.cl.DialTCPPort(ctx, Port)
			if err != nil {
				logf("tunnel: dial host: %v", err)
				c.Close()
				return
			}
			tailcat.ProxyConns(c, rc)
		}()
	}
}

func (p *Peer) Close() error { return p.cl.Close() }
