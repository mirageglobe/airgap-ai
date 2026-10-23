package ari

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoRouteOut(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	open := l.Addr().String()
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	shut := closed.Addr().String()
	closed.Close()
	defer l.Close()

	if err := NoRouteOut([]string{shut}).Run(context.Background()); err != nil {
		t.Errorf("closed address failed the check: %v", err)
	}
	if err := NoRouteOut([]string{shut, open}).Run(context.Background()); err == nil {
		t.Error("an answering address passed the check")
	}
}

// One failed check fails the whole preflight, and env.md records each result.
func TestPreflightFailsClosed(t *testing.T) {
	dir := t.TempDir()
	out, err := Preflight(dir, []Check{
		DataWritable(dir),
		{Name: "broken", Run: func(context.Context) error { return errors.New("nope") }},
	})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err = %v, want broken named", err)
	}
	b, rerr := os.ReadFile(filepath.Join(dir, "env.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, want := range []string{"PASS data folder writable", "FAIL broken: nope"} {
		if !strings.Contains(string(b), want) || !strings.Contains(out, want) {
			t.Errorf("env.md missing %q:\n%s", want, b)
		}
	}
}
