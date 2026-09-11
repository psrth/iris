package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/psrth/iris/client"
	"github.com/psrth/iris/tunnel"
	"tailscale.com/types/logger"
)

func connect(args []string) {
	fs := flag.NewFlagSet("iris connect", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "local listen address")
	fg := fs.Bool("fg", false, "run in the foreground instead of detaching")
	verbose := fs.Bool("v", false, "log tunnel internals")
	fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	token, err := tunnel.Parse(fs.Arg(0))
	if err != nil {
		die("iris connect: %v", err)
	}
	if !*fg {
		if s, ok := servedLocally(token); ok {
			fmt.Print(banner(token.UID, s))
			return
		}
		out, err := spawn("connect", args)
		if err != nil {
			die("iris connect: %v", err)
		}
		fmt.Print(out)
		return
	}

	local, err := net.Listen("tcp", *addr)
	if err != nil {
		die("iris connect: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logf := tunnelLogf(*verbose)
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	peer, err := tunnel.Dial(dialCtx, token.Blob, logf)
	cancel()
	if err != nil {
		die("iris connect: host unreachable: %v", err)
	}
	defer peer.Close()

	s := client.Session{URL: "http://" + local.Addr().String() + "/s/" + token.UID, Key: token.Key}
	if err := store().Save(token.UID, s); err != nil {
		die("iris connect: %v", err)
	}
	if err := writePid(token.UID, "connect"); err != nil {
		die("iris connect: %v", err)
	}
	defer os.Remove(pidfile(token.UID, "connect"))
	announce(banner(token.UID, s))

	ctx, cancel = context.WithCancel(ctx)
	go watch(ctx, cancel, s, logf)
	err = peer.Forward(ctx, local, logf)
	store().Remove(token.UID)
	if err != nil && ctx.Err() == nil {
		die("iris connect: %v", err)
	}
}

func banner(uid string, s client.Session) string {
	return fmt.Sprintf("session  %s\nkey      %s\n", s.URL, s.Key)
}

// servedLocally returns the session when a relay on this machine answers
// for it: the one iris serve recorded, else one on the default address.
func servedLocally(t tunnel.Token) (client.Session, bool) {
	candidates := []client.Session{{URL: "http://" + localAddr + "/s/" + t.UID, Key: t.Key}}
	if s, err := store().Load(t.UID); err == nil {
		candidates = append([]client.Session{s}, candidates...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, s := range candidates {
		if r, err := s.Read(ctx, 0, 1); err == nil && r.OK() {
			store().Save(t.UID, s)
			return s, true
		}
	}
	return client.Session{}, false
}

// watch ends the tunnel once the session is gone: purged or replaced on
// the host, or the host unreachable for five minutes running.
func watch(ctx context.Context, cancel context.CancelFunc, s client.Session, logf logger.Logf) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, pcancel := context.WithTimeout(ctx, 20*time.Second)
		r, err := s.Read(pctx, 0, 1)
		pcancel()
		switch {
		case err != nil:
			if fails++; fails >= 5 {
				logf("iris connect: host unreachable for %d minutes; exiting", fails)
				cancel()
				return
			}
		case r.Status == http.StatusGone || r.Status == http.StatusNotFound || r.Status == http.StatusUnauthorized:
			logf("iris connect: session over (%s); exiting", r.Error())
			cancel()
			return
		default:
			fails = 0
		}
	}
}
