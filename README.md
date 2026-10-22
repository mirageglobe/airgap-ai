# airgap-ai

ari: a small team of local agents that look after a machine with no internet. they find what is broken, fix it from a short list of approved, reversible repairs, check the fix worked, undo it if not, and escalate anything else to a human.

built for the Open Agent Hackathon 2026, track 03 (reasoning architecture).

status: design only. see [docs/design.md](docs/design.md).

## stack

* Zetaris: evidence store the agents query with sql
* Meterless Nightshift: local model runtime; Meterless Scout Intent: repair routing
* go: one binary, nothing to install on the offline machine

## setup

not yet. copy `.env.example` to `.env` and fill in values once the stack exists.
