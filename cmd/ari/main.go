// ari looks after a toy service with no internet. Usage:
//
//	ari run -target DIR -data DIR -catalogue FILE -toyd PATH -health URL [-cycles N]
//	ari status -data DIR
//	ari watch -data DIR
//	ari stop -data DIR
//	ari report -data DIR
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/mirageglobe/airgap-ai/internal/ari"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "report":
		report(os.Args[2:])
	case "status":
		status(os.Args[2:])
	case "watch":
		watch(os.Args[2:])
	case "stop":
		stop(os.Args[2:])
	default:
		usage()
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var cfg ari.Config
	fs.StringVar(&cfg.DataDir, "data", "data", "folder for ari's own files")
	fs.StringVar(&cfg.TargetDir, "target", "target", "toy service folder")
	fs.StringVar(&cfg.Toyd, "toyd", "toyd", "path to the toyd binary")
	fs.StringVar(&cfg.HealthURL, "health", "http://127.0.0.1:8081/health", "toyd health url")
	fs.IntVar(&cfg.Cycles, "cycles", 0, "stop after N cycles (0 runs until stopped)")
	fs.DurationVar(&cfg.Interval, "interval", 5*time.Second, "time between cycles")
	catalogue := fs.String("catalogue", "catalogue/toyd.json", "catalogue file")
	modelURL := fs.String("model-url", os.Getenv("METERLESS_BASE_URL"), "OpenAI-compatible base url; empty uses the rule fixer")
	modelName := fs.String("model", envOr("METERLESS_MODEL", "meterless/auto"), "model or routing mode")
	modelLabel := fs.String("model-label", "nightshift", "feed tag for model lines")
	modelReasoning := fs.String("model-reasoning", os.Getenv("ARI_MODEL_REASONING"), "reasoning_effort to send, e.g. none; empty sends nothing")
	evidenceDSN := fs.String("evidence-dsn", os.Getenv("ARI_EVIDENCE_DSN"), "postgres url of the evidence db; empty keeps rows in memory")
	zetarisURL := fs.String("zetaris-url", os.Getenv("ZETARIS_BASE_URL"), "Zetaris UI origin; empty reads evidence from memory")
	zetarisUser := fs.String("zetaris-user", envOr("ZETARIS_USERNAME", "admin@zetaris.com"), "Zetaris user")
	preflight := fs.Bool("preflight", true, "check there is no route out and every dependency answers before starting")
	fs.Parse(args)

	cat, err := ari.LoadCatalogue(*catalogue)
	if err != nil {
		fail(err)
	}
	checks := []ari.Check{ari.DataWritable(cfg.DataDir), ari.NoRouteOut(ari.OutsideProbes)}
	var fixer ari.Fixer = ari.RuleFixer{}
	if *modelURL != "" {
		m := &ari.Model{
			BaseURL:   *modelURL,
			Token:     os.Getenv("METERLESS_RUNTIME_TOKEN"),
			Name:      *modelName,
			Label:     *modelLabel,
			Reasoning: *modelReasoning,
		}
		fixer = ari.ModelFixer{Model: m}
		checks = append(checks, ari.ModelAnswers(m))
	}
	if *evidenceDSN != "" {
		sink, err := ari.NewPostgresSink(*evidenceDSN)
		if err != nil {
			fail(fmt.Errorf("evidence db: %w", err))
		}
		defer sink.Close()
		cfg.Sink = sink
	}
	if *zetarisURL != "" {
		if cfg.Sink == nil {
			fail(fmt.Errorf("-zetaris-url needs -evidence-dsn: Zetaris reads the rows ari writes there"))
		}
		cfg.RunID = time.Now().UTC().Format("20060102T150405Z")
		z := &ari.Zetaris{BaseURL: *zetarisURL, User: *zetarisUser, Password: os.Getenv("ZETARIS_PASSWORD")}
		cfg.Evidence = ari.ZetarisEvidence{Z: z, Run: cfg.RunID}
		checks = append(checks, ari.ZetarisAnswers(z))
	}
	// A stopped ari stays stopped: refuse before touching anything else.
	if stop := filepath.Join(cfg.DataDir, ari.StopFile); fileExists(stop) {
		fmt.Printf("ari: not starting: stop flag %s is set; remove it to let ari start\n", stop)
		return
	}
	if *preflight {
		out, err := ari.Preflight(cfg.DataDir, checks)
		fmt.Print(out)
		if err != nil {
			fail(err)
		}
	}
	cfg.Feed = os.Stdout
	e, err := ari.New(cfg, cat, fixer)
	if err != nil {
		fail(err)
	}
	runErr := e.Run()
	if err := e.Close(); runErr == nil {
		runErr = err
	}
	if runErr != nil {
		fail(runErr)
	}
}

func report(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	data := fs.String("data", "data", "folder for ari's own files")
	fs.Parse(args)
	out, err := ari.Report(*data)
	if err != nil {
		fail(err)
	}
	fmt.Print(out)
}

func dataFlag(name string, args []string) string {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	data := fs.String("data", "data", "folder for ari's own files")
	fs.Parse(args)
	return *data
}

func status(args []string) {
	out, err := ari.Status(dataFlag("status", args))
	if err != nil {
		fail(err)
	}
	fmt.Print(out)
}

func stop(args []string) {
	data := dataFlag("stop", args)
	by := "unknown"
	if u, err := user.Current(); err == nil {
		by = u.Username
	}
	if err := ari.RequestStop(data, by); err != nil {
		fail(err)
	}
	fmt.Printf("stop flag set in %s; ari halts within a second and stays stopped until the flag is removed\n", data)
}

// watch prints the feed so far, then follows it like tail -f.
func watch(args []string) {
	path := filepath.Join(dataFlag("watch", args), "feed.log")
	f, err := os.Open(path)
	for err != nil {
		if !os.IsNotExist(err) {
			fail(err)
		}
		time.Sleep(time.Second)
		f, err = os.Open(path)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		fmt.Print(line)
		if err == io.EOF {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if err != nil {
			fail(err)
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ari run|status|watch|stop|report [flags]")
	os.Exit(2)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ari:", err)
	os.Exit(1)
}
