package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/psrth/iris/client"
)

// exit codes shared by every session command
const (
	exitError   = 1 // request failed or relay unreachable
	exitOver    = 2 // session read-only or purged
	exitTimeout = 3 // wait: nothing before the deadline
)

// session parses the common -s flag and loads that session.
func session(fs *flag.FlagSet, args []string) client.Session {
	uid := fs.String("s", "", "session uid (default: the only stored one)")
	fs.Parse(args)
	s, err := store().Load(*uid)
	if err != nil {
		die("%s: %v", fs.Name(), err)
	}
	return s
}

// finish prints the relay's body and exits by status.
func finish(name string, r client.Result, err error) {
	if err != nil {
		die("%s: %v", name, err)
	}
	if !r.OK() {
		fmt.Fprintf(os.Stderr, "%s: %s\n", name, r.Error())
		if r.Status == http.StatusConflict || r.Status == http.StatusGone {
			os.Exit(exitOver)
		}
		os.Exit(exitError)
	}
	os.Stdout.Write(r.Body)
	if len(r.Body) > 0 && r.Body[len(r.Body)-1] != '\n' {
		fmt.Println()
	}
}

func post(args []string) {
	fs := flag.NewFlagSet("iris post", flag.ExitOnError)
	var p client.Post
	var files fileParts
	var extra string
	fs.StringVar(&p.Name, "n", "", "your handle (required)")
	fs.StringVar(&p.Role, "role", "assistant", "assistant or user")
	fs.BoolVar(&p.Urgent, "u", false, "urgent: wake peers waiting with -urgent")
	fs.BoolVar(&p.Human, "human", false, "a person should see this before anyone acts")
	fs.IntVar(&p.ReplyTo, "r", 0, "seq this replies to")
	fs.Var(&files, "f", "file part as name:seq (repeatable)")
	fs.StringVar(&extra, "m", "", "extra metadata as a JSON object")
	s := session(fs, args)
	if fs.NArg() != 1 {
		die("iris post: one argument: the text, or - for stdin")
	}
	p.Text = fs.Arg(0)
	if p.Text == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			die("iris post: %v", err)
		}
		p.Text = string(b)
	}
	p.Files = files
	if extra != "" {
		if err := json.Unmarshal([]byte(extra), &p.Extra); err != nil {
			die("iris post: -m: %v", err)
		}
	}
	body, err := p.JSON()
	if err != nil {
		die("iris post: %v", err)
	}
	r, err := s.Post(context.Background(), body)
	finish("iris post", r, err)
}

func read(args []string) {
	fs := flag.NewFlagSet("iris read", flag.ExitOnError)
	since := fs.Int("since", 0, "return messages after this seq")
	limit := fs.Int("limit", 0, "at most this many (relay default 200, max 1000)")
	s := session(fs, args)
	r, err := s.Read(context.Background(), *since, *limit)
	finish("iris read", r, err)
}

func wait(args []string) {
	fs := flag.NewFlagSet("iris wait", flag.ExitOnError)
	since := fs.Int("since", 0, "wake on messages after this seq")
	urgent := fs.Bool("urgent", false, "wake on urgent messages only")
	timeout := fs.Duration("timeout", 0, "give up after this long (default: never)")
	s := session(fs, args)
	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	r, err := s.Wait(ctx, *since, *urgent)
	if errors.Is(err, client.ErrTimeout) {
		os.Exit(exitTimeout)
	}
	finish("iris wait", r, err)
}

func put(args []string) {
	fs := flag.NewFlagSet("iris put", flag.ExitOnError)
	name := fs.String("name", "", "name in the session (default: the file's basename)")
	s := session(fs, args)
	if fs.NArg() != 1 {
		die("iris put: one argument: the path")
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		die("iris put: %v", err)
	}
	defer f.Close()
	if *name == "" {
		*name = filepath.Base(path)
	}
	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype == "" {
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		ctype = http.DetectContentType(head[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			die("iris put: %v", err)
		}
	}
	r, err := s.Put(context.Background(), *name, ctype, f)
	finish("iris put", r, err)
}

func get(args []string) {
	fs := flag.NewFlagSet("iris get", flag.ExitOnError)
	out := fs.String("o", "", "write to this path (default: the name, in the current directory)")
	s := session(fs, args)
	if fs.NArg() != 1 {
		die("iris get: one argument: the file name")
	}
	name := fs.Arg(0)
	if *out == "" {
		*out = filepath.Base(name)
	}
	tmp := *out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		die("iris get: %v", err)
	}
	r, err := s.Get(context.Background(), name, f)
	f.Close()
	if err != nil || !r.OK() {
		os.Remove(tmp)
		finish("iris get", r, err)
	}
	if err := os.Rename(tmp, *out); err != nil {
		die("iris get: %v", err)
	}
	fmt.Println(*out)
}

func files(args []string) {
	fs := flag.NewFlagSet("iris files", flag.ExitOnError)
	s := session(fs, args)
	r, err := s.Files(context.Background())
	finish("iris files", r, err)
}

func end(args []string) {
	fs := flag.NewFlagSet("iris end", flag.ExitOnError)
	s := session(fs, args)
	r, err := s.Terminate(context.Background())
	finish("iris end", r, err)
}

func stop(args []string) {
	fs := flag.NewFlagSet("iris stop", flag.ExitOnError)
	uid := fs.String("s", "", "stop only this session's processes")
	fs.Parse(args)
	lines, err := stopDaemons(*uid)
	if err != nil {
		die("iris stop: %v", err)
	}
	for _, l := range lines {
		fmt.Println(l)
	}
	// give the processes a moment to remove their session entries
	if len(lines) > 0 {
		time.Sleep(200 * time.Millisecond)
	}
}

// fileParts collects -f name:seq flags.
type fileParts []client.FilePart

func (f *fileParts) String() string { return fmt.Sprint(*f) }

func (f *fileParts) Set(v string) error {
	name, seq, ok := strings.Cut(v, ":")
	n, err := strconv.Atoi(seq)
	if !ok || err != nil || name == "" || n < 1 {
		return errors.New("want name:seq")
	}
	*f = append(*f, client.FilePart{Name: name, Seq: n})
	return nil
}
