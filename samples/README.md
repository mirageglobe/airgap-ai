# samples

real runs of the three acceptance scenarios, recorded on the server with Zetaris in the loop and the air gap on (`deploy/vm/demo.sh`). each folder is ari's data folder as it was left:

* `demo-output.txt`: the whole run as printed: preflight, the live feed, the report
* `env.md`: preflight results, including no route out
* `feed.log`: the feed, model words tagged `[ ARI : nightshift ]` when a model is set
* `log.jsonl`: every row, query, Scout contract and command, with exit codes
* `findings.jsonl`: each finding and how it ended
* `state.json`: what `ari status` reads

| folder | fault | what ari did |
|---|---|---|
| `stopped` | toyd killed | restart-service under a Scout contract, done test passed, resolved |
| `badfix` | config and its known good copy corrupted | restore-config failed its done test, undone, re planned contract, failed again, undone, escalated |
| `leak` | memory leak slows health | no catalogue entry, escalated, no command ran |

`nightshift/` holds runs with Meterless Nightshift as the fixer's model provider (`meterless/auto` routing to NVIDIA cloud models, reached on the server's own localhost through a tunnel). The folders `stopped/`, `badfix/` and `leak/` are full data folders: `log.jsonl` tags every step with its agent role (explorer, investigator, fixer, scout, verifier) and records which model Nightshift routed each pick to, with its receipt id. In `badfix` the retry is chosen with the failure as evidence (the only entry, retried once, then escalated); on `leak` the model found no entry and ari escalated. The older `ns-*.txt` transcripts are earlier runs: in `ns-leak.txt` the model picked the wrong repair and Scout refused it (`scope.mismatch`). Those earlier runs asked for a Nemotron model, but Nightshift routes per app profile, so which model served them was not recorded.

`model/` holds the same three scenarios with a local model as the fixer (gemma4 e2b on Ollama, on the server, behind the air gap; labelled `ollama`, standing in for Nightshift). it picked right on `stopped` and `badfix`; on `leak` it picked `restore-config` for a slow service, and Scout refused the contract (`scope.mismatch`) so nothing ran.

`casts/` holds terminal recordings of the three runs with their real timing, for the video: `asciinema play samples/casts/badfix.cast`.

read one without installing anything:

```sh
go run ./cmd/ari report -data samples/badfix
```
