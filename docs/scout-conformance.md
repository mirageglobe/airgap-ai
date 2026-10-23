# Scout Intent in ari

`internal/ari/scout.go` is a Go implementation of the Meterless Scout Intent engine, written from the published spec at [meterless/meterless `engines/scout-intent`](https://github.com/meterless/meterless/tree/main/engines/scout-intent), commit `70ca80fe88ad8d27288501e8dd08b091ed38c095` (2026-10-18). The spec is an implementation spec with no published package and no Go SDK, so the engine is ported, not imported. This page maps the spec to the code and says what is left out.

## Where it sits

The fixer picks a catalogue entry; Scout decides on that pick and signs a contract; the verifier acts only on a contract that verifies. Scout itself executes nothing (spec §14: "It decides, guards, routes, and signs. Executors execute.").

## Mapping

| Spec | ari | Code |
|---|---|---|
| Intent registry (§3) | built from the catalogue: one intent per symptom, `toyd.<symptom>`; `registryVersion` = catalogue size | `NewScout`, `intentOf` |
| Sense (§4) | the input is the explorer's structured symptom, not free text, so stage 1 is a deterministic match; stage 2 rescoring is not needed | `Decide` |
| Confidence bands (§4.4) | 1.00 rules (High), 0.90 a model pick that matches the symptom (High), 0.00 when refused (Reject) | `Decide` |
| Interpret (§5) | parameters bound: finding id and evidence row ids | `Contract.Intent.Parameters` |
| Guard (§6) | flags `route.unknown-entry`, `scope.mismatch`, `guard.injection` (shell or url fragments in a model's reason), `policy.budget`; any flag is high risk and refused, as ari has no approvers | `Decide`, `injectionHints` |
| Route (§7) | tool plan: precondition, apply, done test, undo on failure, for exactly one entry | `Contract.ToolPlan` |
| Recommend and contract (§8.2) | contract with the spec's fields: id, parent, trace, intent, risk, tool plan, model profile, scope with an exact capability set, issued and expires (5 minute TTL), registry and policy versions, `signedBy: scout` | `Contract` |
| Signing (execution-contract.md) | HMAC SHA256 over the JSON encoding without the signature, per deployment secret | `sign` |
| Verification at the boundary | signature, expiry, risk, and the entry inside `scope.capabilities`; the verifier refuses otherwise | `Verify`, engine `handle` |
| Re-planning (§8.3) | a retry after a failed done test gets a new contract with the first as `parentId`, same trace | engine `handle` |
| Telemetry (§9) | `contract.issued`, `contract.verified`, `contract.refused` events in `log.jsonl`, tagged `agent: scout` or `verifier` | engine `handle` |

## Left out, and why

* Stage 2 rescoring, multi intent and clarification triggers: the input is one structured symptom per finding, not a user prompt.
* PII redaction and data class model profiles: no user data passes through ari.
* Approval records: ari runs unattended; anything that would need approval is refused and escalated.
* The secret is generated per process: contracts never leave the process. The check catches a contract changed between deciding and acting, and keeps the verifier from acting on a plan Scout did not issue. It is not a defence against code in the same process.
* The spec's JavaScript eval harness (`SCOUT_IMPL=… npm run evals`) loads a JS module, so it cannot run this Go port. The reproducible feature test stands in for it.

## The feature test

`TestScoutPlantedFaults` in `internal/ari/scout_test.go` plants ten decisions (rule and model picks, wrong symptom, invented entry, injection in the reason, a url in the reason, budget spent); each must be allowed with exactly the right capability or refused with the right flag. `TestScoutContractBoundary` covers tampering, a different deployment's secret, expiry and replan lineage. `TestModelInjectionRefused` shows a refusal end to end with nothing run.

```sh
go test ./internal/ari -run 'Scout|Injection' -v
```
