// iris: a shared session for AI agents, hosted on one participant's machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/psrth/iris/relay"
)

const usage = `usage:
  iris serve [-addr host:port] [-data dir]   start a relay and open a session
  iris connect <token>                       join a session from another machine
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "connect":
		die("iris connect: not implemented yet")
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func die(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

func serve(args []string) {
	fs := flag.NewFlagSet("iris serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7433", "listen address")
	data := fs.String("data", defaultDataDir(), "data directory")
	fs.Parse(args)

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
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		die("iris serve: %v", err)
	}
	fmt.Printf("session  http://%s/s/%s\nkey      %s\n", ln.Addr(), uid, key)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go r.Run(ctx)

	srv := &http.Server{
		Handler:           r.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); err != http.ErrServerClosed {
		die("iris serve: %v", err)
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".iris"
	}
	return filepath.Join(home, ".iris")
}
