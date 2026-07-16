# Contributor documentation

Implementation docs for the EC server — "how the software does it." The
player/referee-facing **rules** live in the
[ecv6-docs](https://github.com/mdhender/ecv6-docs) repository (locally, the
sibling checkout `../docs`) and are the source of truth; nothing here restates a
rule, it links to one.

Implementation detail that isn't player-facing (determinism internals, wire
formats, schemas) is pulled out of the docs repo into here as we work. When
linking to the docs from committed files, use the repository URL, not a `../docs`
relative path — relative paths break when rendered on GitHub.

| Doc | What it covers |
| --- | --- |
| [architecture.md](architecture.md) | Package layout, boundaries, request lifecycle |
| [model.md](model.md) | Domain concept ↔ Go type ↔ storage schema, and the invariants the store guarantees |
| [control-and-ownership.md](control-and-ownership.md) | Who controls and owns what: account → player → controller → faction → asset, with cardinalities and the domain boundary |
| [determinism.md](determinism.md) | The PRNG mechanism spec: seeds, streams, key paths, frozen surfaces, how to add a domain tag, golden vectors |
| [counter-based-prng.md](counter-based-prng.md) | Why the determinism design looks the way it does — reasoning, prior art, trade-offs |
| [reference/system-generation.md](reference/system-generation.md) | How the engine will implement the Genesis System Contents generator: the stage seam and determinism, linking the upstream rules |
| [reference/deposit-generation.md](reference/deposit-generation.md) | How the engine will implement the Genesis Deposits generator: the stage seam and determinism, linking the upstream rules |
| [decisions/](decisions/) | ADRs — one file per hard-to-reverse decision |
| [api/](api/) | The REST surface — spec-first [openapi.yaml](api/openapi.yaml) (application surface drafted; engine deferred), [conventions.md](api/conventions.md), and the [v4 gap analysis](api/v4-gap-analysis.md) |

Fine-grained reference lives in **godoc** (package `doc.go` files and doc
comments next to the code). The Markdown here is the cross-cutting narrative
layer only.
