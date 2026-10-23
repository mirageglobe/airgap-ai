# ari: design

status: running on a Debian 13 server with Zetaris in the loop; acceptance tests 1 to 3 pass end to end under the air gap. Nightshift not wired yet. Open Agent Hackathon 2026, track 03 (reasoning architecture), team of two. build window 2026-10-22 00:00 to 2026-10-27 23:45 utc.

## pitch

a small team of local agents that look after a machine with no internet, find what is broken, and fix it from a short list of approved repairs. it is safe because it is not connected: nothing leaves the machine, and the model runs locally. it works in the open: anyone who logs in sees it, what it is doing and what it has already done. when someone logs in, it greets them and reports.

## hackathon fit

track 03: "turn data, knowledge, memory, and reasoning into reliable decisions and actions".

| rule (required unless noted) | how ari meets it |
| :--- | :--- |
| Zetaris integrated, running locally | the evidence store: the investigator agent queries logs, findings, config and the target's health api through Zetaris sql (rest, `localhost:8888`) |
| at least one Meterless engine in a working feature | Scout Intent routes a finding to a catalogue repair and checks its risk; proof is a reproducible feature test in go. the official Scout eval harness only loads a js module and tests Meterless's own intent fixtures, so passing it means building the whole spec; out of scope |
| Meterless Nightshift as the primary ai provider | every model call goes to the Nightshift runtime (openai compatible, `/v1/chat/completions`, bearer token, `meterless-app` and `meterless-scope` headers) on a local model lane |
| Cursor as the development environment | build in Cursor; keep notes on how it was used for the submission |
| NVIDIA wherever it contributes (optional) | skipped by default: local Nemotron 3.5 Lightning needs about 21 gb ram, which does not fit next to Zetaris (8 gb) on a 24 gb machine |
| a multi agent system, not a single agent or pipeline | five roles with a critique loop, retries and escalation, see agents |
| runs from a clean setup, bounded run time | one setup command, a `--cycles N` demo mode that stops on its own, and a sample mode with recorded runs |
| logs and traces | the live feed and `log.jsonl` show each agent's step, input, output and decision |
| no committed secrets | `.env` in `.gitignore`, `.env.example` with placeholders only |

## visible by design

this must read as a helpful operator, never as something hiding. these are rules, not features.

| rule | how |
| :--- | :--- |
| announces itself | a login banner (motd) says ari is running here, what it may do, and how to stop it |
| named and findable | ari is a plainly named process (`ari`), listed in `ps`, running in a container also named `ari`, listed in `docker ps`; no renaming or masking |
| says it before it acts | every fix is written to the live feed before it runs, then again with the result |
| watchable live | `ari watch` tails the feed in plain text; `ari status` shows state, last cycle and last fix |
| readable record | `log.jsonl` and `findings.jsonl` are plain files in one visible folder; nothing is encrypted or deleted |
| one obvious stop | `ari stop` and a flag file halt it at once, and a stopped ari stays stopped |
| stays where it is put | no copying itself, no persistence outside its container, no hidden files, no scanning beyond its allow list, no privilege escalation, no network |

## agents

| agent | job | uses | hands off to |
| :--- | :--- | :--- | :--- |
| explorer | runs the read only checks each cycle, writes them to the evidence db, flags deviations from baseline | rules only, no model | investigator |
| investigator | asks Zetaris for the evidence around a finding and writes the likely cause with the rows it used | model, `run_sql` via Zetaris | fixer, or escalation if the evidence is too thin |
| fixer | picks one catalogue repair for the cause, with a one line reason | Scout Intent, model | verifier |
| verifier | gates the repair, applies it, runs the done test; undoes on failure and sends it back once | rules, no model | investigator (one retry), escalation, or reporter |
| reporter | writes the one screen report and the escalation notes for the next human | model | the human |

the loop: explorer, investigator, fixer, verifier. a failed done test returns to the investigator with the failure as new evidence; a second failure, a bad pick or no matching repair escalates. shared state is the evidence db, read by every agent through Zetaris.

## scope

| in | out |
| :--- | :--- |
| five agents with one critique loop and one retry | more specialists |
| a catalogue of pre approved, reversible fixes (a handful) | free form commands or self written fixes |
| one local model through Nightshift, thinking off, json output | staged autonomy levels |
| a headless loop until stopped, plus a bounded `--cycles N` demo mode | self scheduling, adaptive cadence |
| cli: `ari status`, `ari watch`, `ari report`, `ari stop` | chat, approvals |
| a short preflight that fails closed | long term memory |
| a real air gap: no outbound route except the model host | fixing anything outside the demo target |

## how it works

1. boot: preflight checks Nightshift answers with a real model, Zetaris answers `SELECT 1`, the data folder is writable, and **there is no route out**: a test connection to the internet must fail. docker `--internal` still allows the gateway ip and host services, so preflight also checks that only the Nightshift and Zetaris ports answer. results go to `env.md`; any failure stops it (fail closed). then it prints its banner.
2. explore: every N seconds the explorer runs a fixed list of read only commands against the target, writes the output to the evidence db and `log.jsonl`, and compares it with a baseline built from the first few cycles.
3. a deviation over threshold becomes a finding. the investigator queries Zetaris for the related logs, config and health rows and writes a likely cause with the rows it used.
4. the fixer routes the cause through Scout Intent to one catalogue repair, as structured json with a one line reason; if the json is bad or nothing matches, it escalates instead of guessing.
5. the verifier gates the fix: the repair must be in the catalogue, marked reversible, its precondition must pass, it must have a done test and an undo, and the per hour fix budget must not be spent. the model's own opinion on whether to act is ignored; in an earlier test, small local models (`gemma4:e2b`, `e4b`) answered `act` on a case that needed `escalate` four runs out of four, so rules decide and the model only investigates, picks from a menu and words the explanation.
6. fix: announce it in the feed, apply it, run the done test; if the test fails, run the undo and send the finding back to the investigator once; a second failure marks it unresolved and escalates. announce the result either way.
7. escalate means ari stops touching that problem and records it, with the evidence rows, for whoever logs in next.
8. report: `ari report` prints one screen: time window, what was normal, what it found, why, what it fixed (with the before and after), what it left alone and why.

## shape

* host: a Debian 13 cloud vm (4 vcpu, 16 gb). earlier note: Zetaris needs 8 gb and 4 cpus given to docker and 15 gb disk; its images are intel/amd, so apple silicon needs rosetta. images are pulled once before the gap closes, like the model.
* Nightshift ships only for mac, windows and dgx spark, so it runs on a laptop either way, serving one local model lane. its default address is `127.0.0.1:8088`; hotspot mode shares it on the network. reaching it from a container or a vm is a day 1 check.
* `ari` systemd unit instead of a container: runs as the unprivileged `ari` user with `IPAddressDeny=any` and `IPAddressAllow=localhost`, a read only system (`ProtectSystem=strict`) and only `/var/lib/ari` writable; it is listed as `ari.service` and in `ps`.
* evidence db: a postgres container on Zetaris's docker network, published on 127.0.0.1 only. ari writes rows to it; Zetaris reads it as datasource `ari_evidence` with a read only role; the investigator queries `ari_evidence.evidence_rows` through the Zetaris UI proxy (`/api/proxy/sql-editor/sqls/run-query`).
* the demo target is a small toy service inside the same `ari` container, run by a separate unprivileged user, so the fixes (restart it, restore a known good config, clear a full scratch folder) never reach beyond it.
* the catalogue is a plain file, one entry per repair: name, symptom signature, precondition, command, done test, undo. adding a repair is adding an entry, and an entry needs its undo or it is rejected.
* model: a small local model from the Nightshift catalogue (12b or smaller), thinking off, json format; picked on day 1 with a 10 fault test.
* the loop is a plain sleep loop inside the container, so no cron or host scheduler is installed.
* language: go. one binary copied onto the offline machine with nothing to install, which fits the air gap. Zetaris is called over its rest api with `net/http`; Nightshift with the official `openai-go` library or plain `net/http`. neither sponsor ships go sample code, so both small clients are written by hand.
* every line of feed output that came from the model carries the tag `[ ARI : nightshift ]`, so it is clear which words are the model's.

## demo script

1. show there is no internet; start the containers; show the `ari` container cannot reach out, and show its banner and `ps` entry.
2. open `ari watch`; let it run a few clean cycles.
3. break the target (stop the toy service, or corrupt its config).
4. watch the agents hand off: the explorer flags it, the investigator cites the Zetaris rows, the fixer picks the repair, the verifier applies it and runs the done test.
5. break it so the first fix fails; watch the undo, the retry through the investigator, then the escalation.
6. break something not in the catalogue; watch it escalate instead of improvising.
7. log in fresh and run `ari report`; it shows every event, the before and after, and what it left alone.

## build plan

each step ends with a done test it can pass by itself.

* [x] `[poc]` verifier loop with a rule based fixer and the toy target, no Zetaris or Nightshift; test: a broken target is fixed, a forced done test failure is undone and escalated (2026-10-22). acceptance tests 1 to 3 pass in `internal/ari`; still open until the stack is wired: Zetaris rows as evidence
* [x] `[ari]` model fixer over an OpenAI compatible endpoint (Nightshift in the product); bad json, no pick or an unreachable model escalates with nothing run; model words only on `[ ARI : nightshift ]` lines; tested against a fake endpoint (2026-10-23)
* [x] `[env]` install Zetaris (`./verify.sh` passes, 2026-10-23) and Nightshift (open) (a local lane answers); then cut the network and repeat both checks; if either fails offline, rethink the air gap claim (2026-10-22)
* [ ] `[env]` reach Nightshift and Zetaris from a container on an `--internal` network (2026-10-22)
* [ ] `[ari]` the container image: debian slim, tools, non root, read only root, the toy target; test: the target runs and a write outside the data folder fails (2026-10-23)
* [x] `[ari]` preflight with the no route out check, fail closed, and the banner; test: it refuses to start with the internet reachable (2026-10-23)
* [x] `[ari]` explorer, baseline, threshold and the evidence db registered in Zetaris; test: a planted fault yields a finding and clean cycles do not (2026-10-23)
* [x] `[ari]` investigator with `run_sql` through Zetaris; test: a finding gets a cause that cites real rows (2026-10-24)
* [ ] `[ari]` fixer with Scout Intent and the catalogue; test: on 10 planted faults it picks the right entry or escalates, with a rule based match as the fallback; this is the engine's feature test (2026-10-24)
* [x] `[ari]` verifier on the real stack: gate, apply, done test, undo, one retry, escalate; test: an entry with no undo is rejected, and a repair whose done test is forced to fail is undone and retried once (2026-10-25)
* [x] `[ari]` reporter and `ari report` (2026-10-25)
* [ ] `[cli]` `ari status`, `ari watch`, `ari stop`, `--cycles N` done (2026-10-23); sample mode open (2026-10-25)
* [ ] `[repo]` one setup command, readme, `.env.example`, sample inputs and outputs, clean setup tested from scratch (2026-10-26)
* [ ] `[demo]` the demo script, run twice end to end; record the video; build the deck; write the sponsor explanation (2026-10-26)
* [ ] `[submit]` declare pre existing components, check for committed secrets, submit on HackOS (2026-10-27)

## acceptance tests

three test cases for ari. names are guesses (toyd, toyd.conf), swap in whatever the toy target really uses.

1. stopped service (happy path)
   * inject: kill toyd
   * expect: explorer flags it within a cycle, investigator cites the health + process rows, fixer picks restart-service
   * done-test: health endpoint 200 within 10s
   * pass: feed shows announce, apply, result in order. finding resolved. report shows before/after. clean cycles before the fault give no finding
2. bad fix, undo, retry, escalate
   * inject: corrupt toyd.conf and also the known-good backup, so restore-config applies but cant pass its done-test
   * expect: fixer picks restore-config, done-test fails, verifier undoes, finding goes back to the investigator once with the failure as evidence, second try fails, escalate
   * pass: undo restores exact bytes (check the hash). exactly 2 attempts, never 3. finding ends unresolved with evidence rows. report lists it under left alone
3. fault not in the catalogue
   * inject: make toyd leak memory till health slows, nothing in the catalogue matches
   * expect: no match or bad json, engine escalates. model never gets a veto (both models said act when it should escalate on 18 oct)
   * pass: nothing outside the catalogue runs (check log.jsonl). escalation note cites the rows. report lists it as left alone

for all 3: every model line has the `[ ARI : nightshift ]` tag, nothing written outside the data folder, `--cycles N` stops on its own.

if time gets tight cut the clean cycle check in 1 first, never 3. its the one that proves the guardrail holds when the model is wrong.

assumptions made while building, to confirm:

* "nothing written outside the data folder" means ari's own files (feed, logs, findings); repairs write only to the paths their catalogue entry declares, since restore-config has to write toyd.conf.
* for these tests to behave as written: restore-config's precondition checks the backup exists, not its hash (a hash check would block it before any attempt), and restart-service matches only "process missing" (so a leaking but running toyd matches nothing).

## submission

one submission per team on HackOS, sent by the primary contact; it can be updated until 2026-10-27 23:45 utc. the team roster locks when the first submission is created. source: HackOS resources, "Submission Form Preview" and "Cursor: Sponsor Overview".

* [ ] project name and description: one sentence plus a short paragraph saying who it is for, what it does and what it produces
* [ ] track: reasoning architecture, named the same in the readme, deck and video
* [ ] team members: full name, HackOS email, github username and role for each, one primary contact; agree the prize split
* [ ] public github repo: all source, readme with setup, usage and dependencies, no committed secrets; recommended: one entry point, at least three sample inputs with their real outputs, logs or traces, `.env.example`, `SAMPLE_MODE=true`
* [ ] demo video, 1 to 4 minutes: the problem and user, the solution, a real run, agents interacting or retrying, key features, how Zetaris and Meterless are integrated; viewable by anyone with the link; no secrets on screen
* [ ] slide deck covering eight points: problem and target users, solution, agent workflow or architecture, technical stack, how Zetaris and Meterless were used, what was unique about the sponsor use, product visuals, future enhancements
* [ ] sponsor explanation: Zetaris and Meterless (how each is integrated and its role), Cursor (how it was used during development), NVIDIA (where it contributes, or why it does not), then what was unique
* [ ] `metadata.json`: the Cursor sponsor folder asks for Cursor usage to be described in it; its format is not published yet
* [ ] pre existing components declared: this design and the earlier small model test
* [ ] final checks: repo is public and runs from a fresh clone, no keys committed, submitted well before the deadline

## limits and risks

* a small model has weak reasoning, so it never decides whether to act; the catalogue and the verifier do, and a wrong menu pick is caught by the done test and undo.
* the menu picking accuracy of the chosen model is untested; the build plan measures it on 10 faults and falls back to rules, so the demo does not depend on it.
* a fix that fails its done test and cannot be undone is the worst case; keep the catalogue to repairs whose undo is trivial (restore a file copy, restart a service).
* offline behaviour of Zetaris and Nightshift is undocumented by both vendors; the env step tests it.
* judges must be able to run it, but the stack needs docker, a Zetaris registry login and the Nightshift app; sample mode and recorded runs cover a judge who cannot install everything.
* two people, six days: cut reporter polish first, never the fix, verify and undo loop.

## open questions

* host: laptop or cloud vm, and whose laptop runs Nightshift?
* does Nightshift serve a container or vm through hotspot mode?
* does Zetaris need internet after the first pull?
* is the NVIDIA prize still on? the 2026-10-21 onboarding recording on HackOS says.
* the full track 03 challenge statement on HackOS; impact is scored against it.
