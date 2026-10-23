# submission draft

working text for the HackOS submission, the deck and the video. keep it true to what runs; update the status lines as things land.

## name and description

**ari**: local agents that keep an offline server healthy.

ari is for teams running servers on isolated, high assurance networks, where nothing can phone home and nobody is watching at 3am. it finds what is broken, fixes it only from a short catalogue of approved, reversible repairs, proves each fix with a done test, undoes it if the test fails, and escalates everything else to the next human with the evidence. it cannot reach the internet, and it says so, out loud, every time it acts.

track: 03, reasoning architecture.

## sponsor explanation

**Zetaris** is ari's evidence store. every cycle the explorer writes what it saw (process state, health, done test results) as rows into a postgres db registered in Zetaris as the datasource `ari_evidence`, read through a read only role. when something breaks, the investigator does not trust its own memory: it asks Zetaris with Lightning SQL for the finding's rows and the health rows of the cycles before it, and writes a cause that cites those row ids. if Zetaris is down or a cited row is missing, ari escalates and runs nothing. Zetaris runs self hosted on the same server from the hackathon bundle, behind the same air gap.

**Meterless Scout Intent** is implemented in go from the published spec, and every repair goes through it. it builds its intent registry from the catalogue, guards the fixer's pick (unknown entry, entry for the wrong symptom, shell or url fragments in a model's reason, hourly budget spent), routes it to a tool plan, and emits an HMAC signed execution contract with a 5 minute TTL and exactly one capability. the verifier refuses anything without a valid contract: bad signature, expired, high risk, or an entry outside the contract's capabilities. a retry after a failed fix gets a re planned contract with the first as its parent. the feature test plants ten decisions and checks each is allowed with the right capability or refused with the right flag.

**Meterless Nightshift** is the fixer's model provider. The headless Nightshift runtime (`meterlessd`) runs on a small arm64 server with its NVIDIA lane, pinned to Nemotron 3.5 Lightning; a restricted ssh tunnel makes it answer on the ari server's own `127.0.0.1:8088`, so ari's air gap stays localhost only. ari sends OpenAI compatible requests with the real runtime token and Nightshift's headers. The model only picks from the catalogue with a one line reason, tagged `[ ARI : nightshift ]` in the feed. All three acceptance scenarios pass with it (`samples/nightshift/`); on the memory leak it picked the wrong repair and Scout refused it.

**Cursor**: (to fill: how Cursor was used during development.)

**NVIDIA**: not used. a local Nemotron did not fit next to Zetaris on the hardware available.

**what is unique**: the model never gets a veto. rules decide whether to act; Scout's signed contract is the only thing the verifier accepts; every fix has an undo and a done test; and the evidence for every decision is a row you can query in Zetaris.

## deck, eight points

1. problem and target users: servers on isolated networks; small teams; outages found hours late; generic AI agents are not allowed near them
2. solution: ari, five local agents with a catalogue of reversible repairs, a done test for every fix, and escalation for everything else
3. agent workflow: explorer, investigator (Zetaris), fixer, Scout contract, verifier (apply, done test, undo, one retry), reporter
4. technical stack: go single binary, Zetaris on docker, postgres evidence db, systemd air gap (`IPAddressDeny=any`), Meterless Scout Intent and Nightshift
5. how Zetaris and Meterless were used: see sponsor explanation
6. what was unique about the sponsor use: Zetaris as the source of truth for the agent's reasoning, not a dashboard; Scout contracts as the only key to act
7. product visuals: feed during a fix, the undo and escalation, `ari report`, `ari status`, the login banner, env.md preflight
8. future enhancements: more catalogue entries per service; the model wording reports; H-MEM for incidents across runs; signed catalogues

## video, about three minutes

footage: `samples/casts/*.cast` are terminal recordings of the three runs with real timing (`asciinema play`).


1. 0:00 the problem in one sentence; the server has no route out (preflight env.md, `IPAddressDeny=any`)
2. 0:20 the login banner and `ari status`
3. 0:35 `deploy/vm/demo.sh stopped`: finding, Zetaris rows cited, Scout contract, apply, done test, resolved
4. 1:20 `demo.sh badfix`: the fix fails, undo restores the exact bytes, retry with a re planned contract, escalation
5. 2:10 `demo.sh leak`: no catalogue entry, escalated, nothing runs
6. 2:40 `ari report`, then `ari stop` and the stop flag
7. no keys or passwords on screen

## pre existing work to declare

* `docs/design.md`, written before the build window
* an earlier small model test (2026-10-18): `gemma4:e2b` and `e4b` answered act on a case that needed escalate four runs out of four, which is why rules decide and the model only picks

## before submitting

* [ ] repo public (owner's call), licence chosen
* [ ] fresh clone: `go test ./...` passes; setup.sh on a clean server
* [ ] no committed secrets (`git log -p | grep -iE "password|token|key"` reviewed)
* [ ] `metadata.json` once its format is published
* [ ] team roster final before the first submission
