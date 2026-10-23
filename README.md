# airgap-ai

ari: a small team of local agents that look after a machine with no internet. they find what is broken, fix it from a short list of approved, reversible repairs, check the fix worked, undo it if not, and escalate anything else to a human.

built for the Open Agent Hackathon 2026, track 03 (reasoning architecture). see [docs/design.md](docs/design.md).

## how it works

* explorer: every cycle checks the target's process and health, writes each observation as a row to the evidence db, and compares health with a baseline from the first clean cycles
* investigator: queries Zetaris for the finding's rows and the baseline rows before it, and writes a cause that cites them; if Zetaris is down or a cited row is missing, it escalates and nothing runs
* fixer: picks one catalogue entry for the cause, by rule or by a model over an OpenAI compatible endpoint (Meterless Nightshift in the product); model words only appear on `[ ARI : nightshift ]` lines
* Scout Intent (Meterless engine): turns the cause and the fixer's pick into a signed execution contract: intent, risk flags (unknown entry, wrong symptom, injection in the model's reason, budget spent), a tool plan, and exactly one capability; anything flagged is refused
* verifier: acts only on a valid contract (signature, not expired, entry inside its capabilities), then checks the stop flag and the entry's precondition; then apply, done test, undo on failure, one retry through the investigator with a re-planned contract, then escalate
* reporter: `ari report` prints what was fixed with before and after rows, and what was left alone and why

## stack

* Zetaris: the evidence store; ari writes rows to a postgres db registered in Zetaris as `ari_evidence`, and the investigator reads them back with Lightning SQL
* Meterless Scout Intent: implemented in go from the published spec (`internal/ari/scout.go`): intent registry from the catalogue, guard, route, HMAC signed execution contracts with a 5 minute TTL, verified at the verifier's boundary
* Meterless Nightshift: the model runtime the fixer calls (OpenAI compatible); the rule fixer runs when no model is set
* go: one binary, nothing to install on the offline machine

## visible by design

* login banner says ari is here, what it may do and how to stop it
* every fix is announced in the feed before it runs, and again with its result
* `ari watch` follows the feed, `ari status` shows state, last cycle and last fix
* `ari stop` (or a `STOP` file in the data folder) halts it within a second, and it stays stopped until the file is removed
* `log.jsonl`, `feed.log`, `findings.jsonl`, `state.json` and `env.md` are plain files in one folder

## the air gap

`ari run` starts with a preflight and refuses to start if anything fails: the data folder is writable, Zetaris answers, the model answers if one is set, and there is no route out (1.1.1.1:443, 8.8.8.8:53 and 9.9.9.9:443 must not answer). on the server the systemd unit enforces it with `IPAddressDeny=any` and `IPAddressAllow=localhost`, so ari and everything it starts reach only the machine itself. results are written to `env.md`.

## layout

* `cmd/ari`: `ari run|status|watch|stop|report`
* `cmd/toyd`: the toy service ari looks after; `SIGUSR1` starts a simulated memory leak
* `internal/ari`: explorer, investigator, fixer, verifier, reporter, Zetaris client, preflight
* `catalogue/toyd.json`: the approved repairs, each with a precondition, done test and undo
* `deploy/vm`: server setup, the systemd unit, login banner and the demo script

## samples

[`samples/`](samples/README.md) holds real recorded runs of the three scenarios; `go run ./cmd/ari report -data samples/badfix` reads one with nothing else installed.

## run the tests

needs go 1.27. the acceptance tests in docs/design.md take about a minute; Zetaris is faked.

```sh
go test ./...
```

## run it on a server

on a Debian 13 server with docker and go 1.27, as root:

1. install Zetaris from the hackathon bundle into `/opt/zetaris-platform` (unzip, `docker login`, `./preflight.sh`, `docker compose pull`, `docker compose up -d`, `./verify.sh`)
2. build ari and toyd into `/opt/ari/bin`, copy `catalogue/toyd.json` to `/opt/ari/catalogue/`
3. `sh deploy/vm/setup.sh`: creates the evidence db, registers it in Zetaris, writes `/etc/ari/ari.conf`, installs the unit and banner
4. `systemctl start ari`, then `ari watch -data /var/lib/ari/data`

the three acceptance scenarios end to end, under the same air gap:

```sh
deploy/vm/demo.sh stopped   # toyd killed: restarted and verified
deploy/vm/demo.sh badfix    # config and its known good copy corrupted: fix fails, undone, retried, escalated
deploy/vm/demo.sh leak      # memory leak: no catalogue entry, escalated, nothing runs
```

to let a model pick the repair, set `METERLESS_BASE_URL`, `METERLESS_RUNTIME_TOKEN` and `METERLESS_MODEL` in `/etc/ari/ari.conf`, and allow the model host with `IPAddressAllow=` in the unit. the model only picks from the catalogue; the gate still decides.
