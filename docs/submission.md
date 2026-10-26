# submission draft

working text for the HackOS submission, the deck and the video. keep it true to what runs; update the status lines as things land.

## name and description

**ari**: local agents that keep an offline server healthy.

ari is for teams running servers on isolated, high assurance networks, where nothing can phone home and nobody is watching at 3am. it finds what is broken, fixes it only from a short catalogue of approved, reversible repairs, proves each fix with a done test, undoes it if the test fails, and escalates everything else to the next human with the evidence. it cannot reach the internet, and it says so, out loud, every time it acts.

track: 03, reasoning architecture.

## sponsor explanation

**Zetaris** is ari's evidence store. every cycle the explorer writes what it saw (process state, health, done test results) as rows into a postgres db registered in Zetaris as the datasource `ari_evidence`, read through a read only role. when something breaks, the investigator does not trust its own memory: it asks Zetaris with Lightning SQL for the finding's rows and the health rows of the cycles before it, and writes a cause that cites those row ids. if Zetaris is down or a cited row is missing, ari escalates and runs nothing. Zetaris runs self hosted on the same server from the hackathon bundle, behind the same air gap.

**Meterless Scout Intent** is implemented in go from the published spec, and every repair goes through it. it builds its intent registry from the catalogue, guards the fixer's pick (unknown entry, entry for the wrong symptom, shell or url fragments in a model's reason, hourly budget spent), routes it to a tool plan, and emits an HMAC signed execution contract with a 5 minute TTL and exactly one capability. the verifier refuses anything without a valid contract: bad signature, expired, high risk, or an entry outside the contract's capabilities. a retry after a failed fix gets a re planned contract with the first as its parent. the mapping to the spec is in `docs/scout-conformance.md`; the feature test plants ten decisions and checks each is allowed with the right capability or refused with the right flag.

**Meterless Nightshift** is the fixer's model provider. The headless Nightshift runtime (`meterlessd`) runs on a small arm64 server; ari asks it for `meterless/auto` and Nightshift routes each pick to one of NVIDIA's cloud models. A restricted ssh tunnel makes it answer on the ari server's own `127.0.0.1:8088`, so the ari process itself only ever reaches localhost; the model call is the one channel that leaves, and only through Nightshift. ari sends OpenAI compatible requests with the runtime's real token and Nightshift's `meterless-app` and `meterless-scope` headers, and `log.jsonl` records, for every pick, the model Nightshift routed to and its compute receipt id. The model only picks from the catalogue with a one line reason, tagged `[ ARI : nightshift ]`. All three scenarios pass with it (`samples/nightshift/`); in one leak run the model picked the wrong repair and Scout refused it. We tried to pin a Nemotron model, but Nightshift binds each app to its routing profile, so the requested model is recorded and not forced. The model is optional by design: rules alone pass all three scenarios fully air gapped, and a local model behind the air gap (gemma4 e2b on Ollama, on the server) works too; Nightshift's DGX runtime also ships packages for serving models locally on an NVIDIA GPU, which would keep the model inside the air gap, though we did not have the hardware to test it. In every mode Scout Intent signs each action on the machine, offline.

**Cursor**: we used the Cursor agent CLI during development as a builder and a code reviewer: it ran code reviews of the engine (`internal/ari/engine.go`, `fixer.go`) and the deploy script (`deploy/recreate.sh`). It built `deploy/local/demo.sh`, the laptop demo that runs all three scenarios with no server, Zetaris, model or root, so anyone can try ari in a minute; and it wrote the README's "try it without servers" section. As a reviewer it found two real bugs we then fixed: in `deploy/recreate.sh`, a failed token copy could leave Nightshift's token empty in ari's config (now it stops with an error); and in the engine, `ari status` reported a failed undo as "undone", telling the operator the server was safe when it might be half changed (now it says "undo failed", with a test). It also scanned the full git history for secrets with gitleaks and manual checks and found no keys or tokens. We checked every finding before acting on it and left out the ones that did not hold.

**NVIDIA**: the models behind Nightshift are NVIDIA's cloud endpoints (build.nvidia.com), set up with an NVIDIA API key in Nightshift's key store; which one answered is in each pick's log line. A local Nemotron did not fit next to Zetaris on the hardware available.

**what is unique**: the model never gets a veto. rules decide whether to act; Scout's signed contract is the only thing the verifier accepts; every fix has an undo and a done test; and the evidence for every decision is a row you can query in Zetaris.

## deck, eight points

1. problem and target users: servers on isolated networks; small teams; outages found hours late; generic AI agents are not allowed near them
2. solution: ari, six roles in one supervised loop, with a catalogue of reversible repairs, a done test for every fix, and escalation for everything else
3. agent workflow: explorer, investigator (Zetaris), fixer, Scout contract, verifier (apply, done test, undo, one retry), reporter
4. technical stack: go single binary, Zetaris on docker, postgres evidence db, systemd air gap (`IPAddressDeny=any`), Meterless Scout Intent and Nightshift
5. how Zetaris and Meterless were used: see sponsor explanation
6. what was unique about the sponsor use: Zetaris as the source of truth for the agent's reasoning, not a dashboard; Scout contracts as the only key to act
7. product visuals: feed during a fix, the undo and escalation, `ari report`, `ari status`, the login banner, env.md preflight
8. future enhancements: more catalogue entries per service; the model wording reports; H-MEM for incidents across runs; signed catalogues; a done test that checks the symptom cleared, not only health

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
* [x] fresh clone: `go test ./...` passes; setup.sh on a clean server (2026-10-26)
* [x] no committed secrets (`git log -p | grep -iE "password|token|key"` reviewed)
* [x] `metadata.json` (no format published; holds the project, track and each sponsor's role)
* [ ] team roster final before the first submission
