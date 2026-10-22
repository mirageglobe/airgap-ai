# airgap-ai

ari: a small team of local agents that look after a machine with no internet. they find what is broken, fix it from a short list of approved, reversible repairs, check the fix worked, undo it if not, and escalate anything else to a human.

built for the Open Agent Hackathon 2026, track 03 (reasoning architecture).

status: proof of concept. the verifier loop (gate, apply, done test, undo, one retry, escalate) runs against a toy service with a rule based fixer; Zetaris and Nightshift are not wired yet. see [docs/design.md](docs/design.md).

## stack

* Zetaris: evidence store the agents query with sql
* Meterless Nightshift: local model runtime; Meterless Scout Intent: repair routing
* go: one binary, nothing to install on the offline machine

## layout

* `cmd/toyd`: the toy service ari looks after; `SIGUSR1` starts a simulated memory leak
* `cmd/ari`: `ari run` and `ari report`
* `internal/ari`: explorer, investigator, fixer, verifier, report
* `catalogue/toyd.json`: the approved repairs

## usage

needs go 1.27.

```sh
go test ./...          # the acceptance tests in docs/design.md, about a minute
go build -o bin/ari ./cmd/ari
go build -o bin/toyd ./cmd/toyd
bin/ari run -target TARGET_DIR -data DATA_DIR -toyd bin/toyd \
  -health http://127.0.0.1:PORT/health -cycles 20 -interval 1s
bin/ari report -data DATA_DIR
```

`TARGET_DIR` holds `toyd.conf` (`port=PORT` and `status=ok`) and its known good copy `toyd.conf.good`. ari writes only `log.jsonl`, `feed.log` and `findings.jsonl` in `DATA_DIR`.
