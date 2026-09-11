package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// readyEnv names the fd a spawned child reports readiness on.
const readyEnv = "IRIS_READY_FD"

// spawn re-executes the binary detached, as `iris <name> -fg args...`,
// with its output in the log directory. It returns whatever the child
// wrote to its ready pipe before closing it, or an error if the child
// exited first.
func spawn(name string, args []string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	logPath := filepath.Join(dataDir(), "log", name+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return "", err
	}
	logf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer logf.Close()
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer r.Close()

	cmd := exec.Command(exe, append([]string{name, "-fg"}, args...)...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.ExtraFiles = []*os.File{w}
	cmd.Env = append(os.Environ(), readyEnv+"=3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		w.Close()
		return "", err
	}
	w.Close()
	out, _ := io.ReadAll(r)
	cmd.Process.Release()
	if len(out) == 0 {
		return "", fmt.Errorf("%s: %s", name, lastLine(logPath))
	}
	return string(out), nil
}

// ready is where a foreground command reports its startup lines: the
// spawn pipe when there is one, else stdout.
func ready() *os.File {
	if os.Getenv(readyEnv) == "3" {
		return os.NewFile(3, "ready")
	}
	return os.Stdout
}

// announce writes the startup lines and closes the pipe, if any.
func announce(lines string) {
	f := ready()
	fmt.Fprint(f, lines)
	if f != os.Stdout {
		f.Close()
	}
}

func lastLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return "exited before it was ready; see " + path
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return lines[len(lines)-1]
}

// pidfile records a daemon's pid as <dir>/run/<uid>.<name>.
func pidfile(uid, name string) string {
	return filepath.Join(dataDir(), "run", uid+"."+name)
}

func writePid(uid, name string) error {
	p := pidfile(uid, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// stopDaemons signals every recorded daemon, or only uid's when set, and
// returns a line per stop. Dead entries are removed silently.
func stopDaemons(uid string) ([]string, error) {
	dir := filepath.Join(dataDir(), "run")
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		id, name, ok := strings.Cut(e.Name(), ".")
		if !ok || (uid != "" && id != uid) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, _ := strconv.Atoi(string(b))
		if pid > 0 && syscall.Kill(pid, syscall.SIGTERM) == nil {
			out = append(out, fmt.Sprintf("stopped %s %s (pid %d)", name, id, pid))
		}
		os.Remove(path)
	}
	return out, nil
}
