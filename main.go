// iris: a shared session for AI agents, hosted on one participant's machine.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/psrth/iris/relay"
	"github.com/psrth/iris/tunnel"
	"tailscale.com/types/logger"
)

// version is set by the release build.
var version = "dev"

// localAddr is where iris serve listens by default and where iris connect
// looks for a session before opening a tunnel.
const localAddr = "127.0.0.1:7433"

const usage = `usage:
  iris serve [-addr host:port] [-data dir] [-derp host,...] [-v]
        start a relay, open a session, print its pairing token
  iris connect [-addr host:port] [-v] <token>
        join a session; print its local URL and key
  iris -version
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
		connect(os.Args[2:])
	case "-version", "--version":
		fmt.Println("iris", version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func die(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

func tunnelLogf(verbose bool) logger.Logf {
	if verbose {
		return log.Printf
	}
	return logger.Discard
}

func serve(args []string) {
	fs := flag.NewFlagSet("iris serve", flag.ExitOnError)
	addr := fs.String("addr", localAddr, "listen address")
	data := fs.String("data", defaultDataDir(), "data directory")
	derp := fs.String("derp", "", "self-hosted DERP server hostname(s), comma-separated")
	verbose := fs.Bool("v", false, "log tunnel internals")
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
	fmt.Println(tunnel.Token{Blob: remote.Blob(), UID: uid, Key: key})

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

func connect(args []string) {
	fs := flag.NewFlagSet("iris connect", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "local listen address")
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
	if servedLocally(token) {
		fmt.Printf("session  http://%s/s/%s\nkey      %s\n", localAddr, token.UID, token.Key)
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
	fmt.Printf("session  http://%s/s/%s\nkey      %s\n", local.Addr(), token.UID, token.Key)
	if err := peer.Forward(ctx, local, logf); err != nil {
		die("iris connect: %v", err)
	}
}

// servedLocally reports whether the token's session is answered by a relay
// on this machine at localAddr, in which case no tunnel is needed.
func servedLocally(t tunnel.Token) bool {
	req, err := http.NewRequest("GET", "http://"+localAddr+"/s/"+t.UID+"?limit=1", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+t.Key)
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".iris"
	}
	return filepath.Join(home, ".iris")
}
