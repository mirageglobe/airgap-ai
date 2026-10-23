package ari

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OutsideProbes are public addresses that must not answer from an air
// gapped machine.
var OutsideProbes = []string{"1.1.1.1:443", "8.8.8.8:53", "9.9.9.9:443"}

// Check is one preflight check; Run returns nil when it passes.
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

// Preflight runs every check, writes the results to env.md in the data
// folder, and fails closed: any failed check is an error.
func Preflight(dataDir string, checks []Check) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# ari preflight %s\n\n", time.Now().UTC().Format(time.RFC3339))
	var failed []string
	for _, c := range checks {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := c.Run(ctx)
		cancel()
		if err != nil {
			failed = append(failed, c.Name)
			fmt.Fprintf(&b, "* FAIL %s: %v\n", c.Name, err)
		} else {
			fmt.Fprintf(&b, "* PASS %s\n", c.Name)
		}
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return b.String(), err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "env.md"), []byte(b.String()), 0o644); err != nil {
		return b.String(), err
	}
	if len(failed) > 0 {
		return b.String(), fmt.Errorf("preflight failed: %s", strings.Join(failed, ", "))
	}
	return b.String(), nil
}

// NoRouteOut passes only if none of the addresses accept a connection.
func NoRouteOut(addrs []string) Check {
	return Check{Name: "no route out (" + strings.Join(addrs, ", ") + ")", Run: func(ctx context.Context) error {
		var d net.Dialer
		for _, a := range addrs {
			dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			c, err := d.DialContext(dctx, "tcp", a)
			cancel()
			if err == nil {
				c.Close()
				return fmt.Errorf("%s answered: this machine can reach the internet", a)
			}
		}
		return nil
	}}
}

func DataWritable(dataDir string) Check {
	return Check{Name: "data folder writable", Run: func(context.Context) error {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return err
		}
		p := filepath.Join(dataDir, "env.md")
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return err
		}
		return f.Close()
	}}
}

func ZetarisAnswers(z *Zetaris) Check {
	return Check{Name: "zetaris answers SELECT 1", Run: func(ctx context.Context) error {
		rows, err := z.Query(ctx, "SELECT 1 AS one", 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0]["one"] != "1" {
			return fmt.Errorf("unexpected result %v", rows)
		}
		return nil
	}}
}

// ModelAnswers checks the OpenAI compatible endpoint lists models.
func ModelAnswers(m *Model) Check {
	return Check{Name: "model endpoint answers", Run: func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(m.BaseURL, "/")+"/models", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+m.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("models: " + resp.Status)
		}
		return nil
	}}
}
