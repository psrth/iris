package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/psrth/iris/client"
	"github.com/psrth/iris/relay"
	"github.com/psrth/iris/tunnel"
	"tailscale.com/types/logger"
)

func tunnelLogf(verbose bool) logger.Logf {
	if verbose {
		return log.Printf
	}
	return logger.Discard
}

func serve(args []string) {
	fs := flag.NewFlagSet("iris serve", flag.ExitOnError)
	addr := fs.String("addr", localAddr, "listen address")
	data := fs.String("data", dataDir(), "data directory")
	derp := fs.String("derp", "", "self-hosted DERP server hostname(s), comma-separated")
	fg := fs.Bool("fg", false, "run in the foreground instead of detaching")
	verbose := fs.Bool("v", false, "log tunnel internals")
	fs.Parse(args)

	if !*fg {
		out, err := spawn("serve", args)
		if err != nil {
			die("iris serve: %v", err)
		}
		fmt.Print(out)
		return
	}

	lim := relay.DefaultLimits
	lim.SessionsPerIPPerHour = 0 // provisioning is host-only; no IP limit
	r, err := relay.Open(relay.Config{DataDir: *data, Limits: lim})
	if err != nil {
		die("iris serve: %v", err)
	}
	defer r.Close()

	uid, key, err := r.NewSession()
	if err != nil {
		die("iris serve: %v", err)
	}
	local, err := net.Listen("tcp", *addr)
	if err != nil {
		die("iris serve: %v", err)
	}
	var hosts []string
	if *derp != "" {
		hosts = strings.Split(*derp, ",")
	}
	remote, err := tunnel.Listen(hosts, tunnelLogf(*verbose))
	if err != nil {
		die("iris serve: tunnel: %v", err)
	}
	// The host's own agents connect through the store, so a non-default
	// -addr still resolves locally.
	if err := store().Save(uid, client.Session{URL: "http://" + local.Addr().String() + "/s/" + uid, Key: key}); err != nil {
		die("iris serve: %v", err)
	}
	if err := writePid(uid, "serve"); err != nil {
		die("iris serve: %v", err)
	}
	defer os.Remove(pidfile(uid, "serve"))
	announce(tunnel.Token{Blob: remote.Blob(), UID: uid, Key: key}.String() + "\n")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go r.Run(ctx)

	servers := []*http.Server{
		{Handler: r.Handler()},
		{Handler: r.RemoteHandler()},
	}
	errc := make(chan error, len(servers))
	for i, ln := range []net.Listener{local, remote} {
		srv := servers[i]
		srv.ReadHeaderTimeout = 10 * time.Second
		srv.BaseContext = func(net.Listener) context.Context { return ctx }
		go func() { errc <- srv.Serve(ln) }()
	}
	select {
	case <-ctx.Done():
	case err := <-errc:
		die("iris serve: %v", err)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(shutdown)
	}
}
