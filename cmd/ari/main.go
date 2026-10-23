// ari looks after a toy service with no internet. Usage:
//
//	ari run -target DIR -data DIR -catalogue FILE -toyd PATH -health URL [-cycles N]
//	ari report -data DIR
package main

import (
	"flag"
	"fmt"
	"os"
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
	evidenceDSN := fs.String("evidence-dsn", os.Getenv("ARI_EVIDENCE_DSN"), "postgres url of the evidence db; empty keeps rows in memory")
	zetarisURL := fs.String("zetaris-url", os.Getenv("ZETARIS_BASE_URL"), "Zetaris UI origin; empty reads evidence from memory")
	zetarisUser := fs.String("zetaris-user", envOr("ZETARIS_USERNAME", "admin@zetaris.com"), "Zetaris user")
	fs.Parse(args)

	cat, err := ari.LoadCatalogue(*catalogue)
	if err != nil {
		fail(err)
	}
	var fixer ari.Fixer = ari.RuleFixer{}
	if *modelURL != "" {
		fixer = ari.ModelFixer{Model: &ari.Model{
			BaseURL: *modelURL,
			Token:   os.Getenv("METERLESS_RUNTIME_TOKEN"),
			Name:    *modelName,
			Label:   *modelLabel,
		}}
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
		cfg.Evidence = ari.ZetarisEvidence{
			Z:   &ari.Zetaris{BaseURL: *zetarisURL, User: *zetarisUser, Password: os.Getenv("ZETARIS_PASSWORD")},
			Run: cfg.RunID,
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ari run|report [flags]")
	os.Exit(2)
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
