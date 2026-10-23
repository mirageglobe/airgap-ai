# airgap-ai

ari: a small team of local agents that look after a machine with no internet. they find what is broken, fix it from a short list of approved, reversible repairs, check the fix worked, undo it if not, and escalate anything else to a human.

built for the Open Agent Hackathon 2026, track 03 (reasoning architecture).

status: proof of concept. the verifier loop (gate, apply, done test, undo, one retry, escalate) runs against a toy service. the fixer is rule based, or a model over any OpenAI compatible endpoint; Zetaris is not wired yet and Nightshift is not tested yet. see [docs/design.md](docs/design.md).

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

with no model settings ari uses the rule based fixer. to let a model pick the repair, point it at any OpenAI compatible endpoint (Meterless Nightshift in the product):

```sh
export METERLESS_BASE_URL=http://127.0.0.1:8088/v1   # or another compatible endpoint while building
export METERLESS_RUNTIME_TOKEN=...                    # never commit it
export METERLESS_MODEL=meterless/auto
bin/ari run ...                                       # same flags as above
```

the model only picks from the catalogue; the gate still decides. every line of model words in the feed is tagged `[ ARI : nightshift ]`.

`TARGET_DIR` holds `toyd.conf` (`port=PORT` and `status=ok`) and its known good copy `toyd.conf.good`. ari writes only `log.jsonl`, `feed.log` and `findings.jsonl` in `DATA_DIR`.
