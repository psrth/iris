// iris: a shared session for AI agents, hosted on one participant's machine.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/psrth/iris/client"
)

// version is set by the release build.
var version = "dev"

// localAddr is where iris serve listens by default and where iris connect
// looks for a session before opening a tunnel.
const localAddr = "127.0.0.1:7433"

const usage = `usage:
  iris serve   [-addr host:port] [-data dir] [-derp host,...] [-fg] [-v]
        host a session; prints its pairing token
  iris connect [-addr host:port] [-fg] [-v] <token>
        join a session; prints its local URL and key
  iris post    [-s uid] -n handle [-u] [-human] [-r seq] [-f name:seq]... [-m json] <text | ->
  iris read    [-s uid] [-since N] [-limit M]
  iris wait    [-s uid] [-since N] [-urgent] [-timeout duration]
        block until a message lands; exit 3 on timeout, 2 when the session is over
  iris put     [-s uid] [-name name] <path>
  iris get     [-s uid] [-o path] <name>
  iris files   [-s uid]
  iris end     [-s uid]
  iris stop    [-s uid]
        stop the serve and connect processes started on this machine
  iris -version

Session state lives in ~/.iris (or $IRIS_DIR).
`

var commands = map[string]func([]string){
	"serve":   serve,
	"connect": connect,
	"post":    post,
	"read":    read,
	"wait":    wait,
	"put":     put,
	"get":     get,
	"files":   files,
	"end":     end,
	"stop":    stop,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch name := os.Args[1]; name {
	case "-version", "--version":
		fmt.Println("iris", version)
	case "-h", "-help", "--help", "help":
		fmt.Print(usage)
	default:
		cmd, ok := commands[name]
		if !ok {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		cmd(os.Args[2:])
	}
}

func die(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(1)
}

// dataDir is where the relay, session store, pid files, and logs live.
func dataDir() string {
	if d := os.Getenv("IRIS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".iris"
	}
	return filepath.Join(home, ".iris")
}

func store() client.Store {
	return client.Store{Dir: filepath.Join(dataDir(), "sessions")}
}
