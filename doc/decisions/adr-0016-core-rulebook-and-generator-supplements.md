# ADR-0016: The rulebook splits into a core and generator supplements

- **Status:** accepted
- **Date:** 2026-07-15

## Context

CLAUDE.md rule 3 says every game-engine feature is grounded in a rule in the
rulebook, and that missing rules are written upstream first. For cluster
generation, that has quietly inverted.

**The inversion is live and measurable.** The rulebook's
[cluster reference](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
says only that "the quantity, quality, and type of deposits a planet carries are
still to be documented." Meanwhile [`doc/reference/deposit-generation.md`](../reference/deposit-generation.md)
in this repository carries the complete table: deposit counts per planet type,
per-deposit quantities, and the exact yield ladder — including that gas giants in
orbits 6–7 yield 5–6% while every other orbit is halved. That single fact tells a
player where to settle.

So today a player who reads the rulebook learns nothing about deposits, and a
player who reads this repository learns the optimal expansion target. This
repository is public and MIT-licensed, so nothing is secret; the asymmetry is not
about leaking. It is that **playing well currently requires reading Go.** That is
the defect this ADR exists to fix.

The pending upstream trim ([ecv6-docs#1](https://github.com/mdhender/ecv6-docs/issues/1))
would widen it. It removes the testbed callout, the reserved-stream callout, and
the per-orbit planet/habitability table from `cluster.md`, intending to backport
"once the design settles." Removing the layout table from the rulebook while it
remains published here moves *more* strategic information behind a code-read.
That issue needs a backport destination; it does not have one today.

Two further forces:

- **The rulebook is welded to one generator.** `cluster.md` documents stellar
  density tiers, the radius table, minimum spacing, a ten-orbit layout, a
  habitability table, and three abundance knobs as if they were the rules of the
  game. They are the parameters of *one* generator. A different placement
  generator — spiral arms, lobes — would have no "stellar density" at all. Every
  tuning pass therefore churns the rulebook.
- **Stability and experimentation are in tension.** Once a game persists, its
  generation is a reproducibility contract. But generators are exactly what we
  need room to iterate on during alpha. One undifferentiated rulebook cannot be
  both the stable contract and the experimental surface.

## Decision

**The rulebook splits into a core and a set of generator supplements. Both are
published upstream and player-facing.**

**The core defines schema and vocabulary** — what a cluster *is*, in terms every
generator shares:

- the hex map, axial `(q, r)` coordinates, the distance metric, and directions;
- a cluster holds `N` systems, `N` set by the GM (the allowed range and default
  belong to the placement generator);
- a system has **ten orbits**, numbered 1–10 innermost outward; each holds one
  planet or is empty;
- the planet **types** that exist: rocky, asteroid belt, gas giant;
- **habitability** is a per-planet number; higher is more habitable;
- planets carry deposits of fuel, metals, and non-metals; each deposit has a
  quantity and a yield;
- generation is reproducible from the game's seeds;
- **the GM chooses a generator for each stage of generation.**

**Orbit count, planet types, and habitability are schema, not generator output.**
Downstream rules — orders, the economy — reference orbit numbers and planet types
directly, so a generator free to vary them would break every rule that names them.
The generator decides *which* planet sits in *which* orbit and *how habitable* it
is; the shape and the vocabulary are core. Core also does **not** say deposits are
"shaped by the abundance settings" — that would couple core to one generator. Core
says a deposit has a quantity and a yield; the deposit generator decides how they
are set.

**A supplement defines one generator** — its settings, how it turns them into a
map, its tables, and its distributions. Resource *types* are core because the
economy references them; how much and where is the generator's business.

The line: **core defines the schema and the vocabulary; supplements define the
values and the knobs.**

**Supplements are first-class published rules, not appendices.** Discoverability
is a fairness surface: a player who reads the core and never finds the supplement
is precisely the disadvantaged player this ADR exists to prevent.

**Generation is staged, and each stage's generator is chosen independently** —
placement, system contents, deposits. The interface between stages is defined:
contents emits planet type and habitability per orbit; deposits consumes
`(planet type, orbit)`. That seam already exists in the current design.

**The v1 generators are codenamed Genesis.** Genesis is a **family** name
covering the three testbed generators — not a single versioned unit. Each stage
versions independently, so a game may run Genesis Deposits v2 against Genesis
Placement v1, or mix a Genesis stage with a generator from another family.
"Genesis v1" is shorthand for *all three stages at v1* and must not be used where
a specific stage's version is meant; a game records three `(generator, version)`
pairs, not one.

**Each stage is published as its own supplement page**, under
`content/reference/generators/genesis/` upstream:

| Stage           | Page                | Covers                                                                    |
| --------------- | ------------------- | ------------------------------------------------------------------------- |
| Placement       | `placement.md`      | Radius, stellar density tiers, minimum spacing, placement and its failure mode |
| System contents | `system-contents.md`| Which planet sits in which orbit, and its habitability value              |
| Deposits        | `deposits.md`       | Counts, quantities, the yield ladder, the abundance knobs                 |

One page per stage because **status and version are per-stage**: a single page
cannot coherently carry "placement stable at v1, deposits draft at v1".

The path uses `generators`, the game's own vocabulary, rather than `supplements`,
which is an editorial term. The core tells players the GM chooses a *generator*,
so that is the word they will look for in the navigation. **Supplement** remains
the name for the document class in decision records and contributor guidance —
what the page *is*; **generator** is what it *describes*.

**Each generator gets its own seed root** via
`Derive(stageTag, generatorID, version)` — the stage's domain tag, then the
generator's identity and version. Below that root a generator owns its addressing
entirely. The global domain-tag registry and the key-path hash encoding remain
frozen ([ADR-0001](adr-0001-counter-based-prng.md),
[../determinism.md](../determinism.md)); only the *root* addressing is a global
frozen surface.

The deposit stage has no domain tag today — the registry holds `TagCluster`,
`TagSystem`, and `TagPlayer`. Whether deposits hang under `TagSystem` with a
distinct generator id, or take an appended `TagDeposit`, is an implementation
question for E1. Appending is free while no game exists, and the registry is
append-only.

**Supplements carry a status and a version.** A supplement is **draft** while no
game depends on it and may change freely; it becomes **stable** once a game
depends on it, and tuning a stable supplement means publishing a new version,
never editing the old one.

**A game records and publishes which generators and versions it ran**, or players
cannot tell which supplement applies to them.

## Consequences

- **Fairness becomes auditable.** A supplement is complete when reading the code
  adds nothing strategic. Any behavior that changes optimal play and is absent
  from the supplement is a **bug**, not a documentation gap. That is testable in
  a way "is this player-facing?" never was.
- **Rule 3 is restated, not weakened.** Engine behavior is grounded in the core
  rulebook **or** a published supplement. Supplements are upstream, so grounding
  still means grounded in the docs.
- **The repo split gets a sharper test.** Mechanism stays here; odds go upstream.
  Knowing the SHA-256 → PCG derivation confers no advantage — it cannot be
  inverted, and a player holding the seeds would already know everything. Knowing
  the yield ladder is directly actionable. The seeds keep the deal secret; the
  supplement publishes the odds.
- **`doc/reference/deposit-generation.md` and `doc/reference/system-generation.md`
  migrate upstream** as supplement content — into `genesis/deposits.md` and
  `genesis/system-contents.md` respectively. What remains here is mechanism: key
  paths, hashing, the tag registry.
- **`cluster.md` loses most of its substance.** Density tiers, the radius table,
  minimum spacing, the orbit/habitability table, and the abundance knobs all move
  to supplements; what is left is schema plus pointers. This resolves
  [ecv6-docs#1](https://github.com/mdhender/ecv6-docs/issues/1) by giving its
  trim a destination — the material moves rather than disappearing.
- **It supersedes part of ecv6-docs#1.** That issue directs that "the generation
  settings section (radius, spacing, abundance) stays as-is — those are GM-facing
  knobs." Being GM-facing is not the test: those knobs are *Genesis's* settings,
  not the game's, and a different placement generator would have no stellar
  density at all. They move to the supplements. The issue's **schema** keep-list —
  ten orbits, planet types, habitability, that deposits exist — stands, and the
  core list above reflects it.
- **Frozen surfaces get scoped.** The tag registry and hash encoding stay
  globally frozen. Each generator's internal key paths freeze per version, on
  their own schedule. This lowers the stakes on addressing decisions inside a
  draft generator.
- **Materialize-vs-compute partly dissolves.** That open question in
  [epic #65](https://github.com/mdhender/ecv6-api/issues/65) was dangerous
  because retuning would rewrite existing maps. With generator and version
  recorded and versions immutable once referenced, compute-on-read is safe:
  retuning means publishing v2, never editing v1.
- **The core risks reading as contentless.** With exactly one generator, the
  supplement *is* the rulebook and the core is a table of contents. The split
  buys optionality and scoped freezing, not immediate clarity. It pays as a split
  only once a second generator exists.
- **The stage interface becomes a compatibility surface.** Independently
  versioned generators must agree on what contents hands to deposits. That is the
  tax on composability.
- **New scope: the game must publish its generators.** Which generators and
  versions a game runs is a game-visible fact — it belongs in the rulebook's game
  reference and eventually on the API's game resource.
- **Alpha buys the draft period free.** CLAUDE.md already treats alpha data as
  disposable, so draft supplements can churn now. The versioning mechanism only
  has to bite when games stop being disposable.
- **The abundance model becomes Genesis Deposits v1 (draft)** — published upstream
  at `content/reference/generators/genesis/deposits.md`, with fuel, metals, and
  non-metals abundance as its parameters.
- **Three pages is deliberate over-engineering, and that is affordable.** With one
  family at v1 and every stage draft, the split earns nothing today. Diátaxis
  makes reference material cheap to restructure as we learn, and per-stage
  versioning is the thing that would be expensive to retrofit — a merged page
  would have to be torn apart the first time one stage versions ahead of another.

## Alternatives considered

- **Keep one rulebook and document the current generator as the rules.**
  Rejected. It welds the rulebook to one generator, churns upstream on every
  tuning pass, and cannot be simultaneously the stable reproducibility contract
  and the experimental surface.
- **Keep generator rules engine-side under `/doc`.** Rejected. That is the
  present state, and it is the asymmetry: it requires reading Go to play well.
- **Supplements as an appendix or a lower-status section.** Rejected. A
  supplement players do not find recreates the disadvantage one level down.
  Discoverability is part of the fairness guarantee, not a presentation detail.
- **Put the generator id and version in the key path rather than the derived seed
  root.** Rejected. It welds generator identity into the global frozen encoding,
  and `Derive` already gives each generator a private subtree it can address
  freely — which is exactly the room to experiment the split is for.
- **Abundance as part of the key path.** Rejected (design-level, recorded here
  for the record). Addressing identifies *what* is drawn; abundance parameterizes
  the distribution the draw is transformed through. Keeping it out means a knob
  acts as a lens on a fixed random field — turn metals to rich and the
  metal-heavy systems stay metal-heavy, just more so — rather than re-rolling the
  map on every nudge.
