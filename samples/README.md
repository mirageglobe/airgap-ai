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

read one without installing anything:

```sh
go run ./cmd/ari report -data samples/badfix
```
