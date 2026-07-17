# ADR-0017: Generator identity is provenance, not entropy; home systems are generated on demand

- **Status:** accepted
- **Date:** 2026-07-17
- **Amends:** [ADR-0016](adr-0016-core-rulebook-and-generator-supplements.md)

## Context

Issue [#90](https://github.com/mdhender/ecv6-api/issues/90) introduces a **mix-and-match
generator model**: a catalog of `ClusterGenerator` / `SystemGenerator` / `PlanetGenerator`,
each identified by a UUID the client selects, driven by typed `Knobs`, so a GM composes a
setup from independent generators instead of running one welded pipeline. Two assumptions in
ADR-0016 do not survive contact with that model.

**First — identity in the seed root.** ADR-0016 gives each generator its seed root via
`Derive(stageTag, generatorID, version)`, folding the generator's identity and version into
the *derived root* (it explicitly kept them out of the frozen key-path encoding but kept them
in the root, and rejected the alternative of dropping them). That assumed `generatorID` was a
small integer. In #90 a generator's identity is a **UUID** — a client-facing selection handle
— and its version is a semver string. A UUID is 128-bit; the root addressing uses `int64`
keys. Folding a UUID into the root drags a client-facing selector into a frozen surface for
**no reproducibility benefit**: a game runs exactly one generator per stage and records which,
so the recorded selection — not seed isolation — is what makes the result reproducible.

**Second — the home-system template.** Genesis produces a fixed per-game **home-system
template**, stored in the `home_template` / `home_template_deposit` tables at a sentinel
`(q, r)`, that founding later copies onto a faction's system. The mix-and-match workflow takes
a different path. The GM generates the cluster, is satisfied, then **picks a system, rebuilds
it with a home-system generator** (replacing its contents), and **only then assigns the
faction** to it. There is no template to copy; there is a generator to run.

## Decision

### 1. Generator identity and version are selection and provenance only — never entropy

- A generator's `Identity` (UUID, name, description, version) is how a client **selects** an
  implementation and how a game **records** what it ran. It is not an input to randomness.
- Seeds derive from the game root and the frozen **stage tags** (`TagCluster`, `TagSystem`,
  `TagDeposit`) plus coordinate keys (`(q, r)`, orbit) — nothing else. The per-stage root
  becomes `Derive(stageTag)`, addressed below by coordinate. **This amends ADR-0016**:
  `Derive(stageTag, generatorID, version)` → `Derive(stageTag)`; `generatorID` and `version`
  leave the derived root.
- Reproducibility is unaffected. A game runs one generator per stage; the
  `(generator, version, knobs)` it ran are recorded. Same game seeds + same recorded selection
  + same knobs → same output, because the generator is a **recorded input**, not because its id
  was hashed into the seeds.
- Two generators at the same stage now draw from the same root and differ by algorithm alone.
  That is fine: only one runs per stage per game, so there is no stream to collide over. The
  "private subtree to experiment in" ADR-0016 wanted still exists — it is the whole per-stage
  subtree, owned by whichever generator runs.

### 2. Home systems are generated on demand; the template is retired

- There is no per-game home-system template. A **home-system generator is an ordinary
  `SystemGenerator`** whose algorithm produces a home layout. The founding sequence is: the GM
  **(1) picks an already-placed system, (2) rebuilds it** by running the home-system generator,
  which **replaces that system's contents** — its planets and their deposits — with the
  generator's output (a targeted delete-and-insert of the `planet` / `deposit` rows for that
  `(q, r)`), **(3) then assigns the faction** to the rebuilt system. Rebuild precedes
  assignment: the system is made into a home *before* a faction is placed on it.
- This is a **setup-time (turn 0)** operation. System contents settle during setup — cluster
  generation first, then any home-system overwrites at founding — and freeze at the start of
  play (turn 1). Overwriting during setup establishes start-of-life state; it is not a
  mid-game mutation.
- The overwrite is deterministic. Per decision 1, addressing is by coordinate, not by
  generator id, so the home-system generator roots at the **same per-`(q, r)` seed** as any
  other system generator for that system. A home system is reproducible from the game seeds +
  the recorded home-generator selection + `(q, r)`.

### 3. Per-system contents provenance

Because a system's contents may come from either the stage generator (during cluster
generation) or a home-system generator (at founding), the game must record **which generator
produced each system's contents**, not only the three game-level stage selections.
`game_generator` (PK `(game_id, stage)`) captures the stage-level selections; home overwrites
need per-system provenance keyed by `(game_id, q, r)`. The exact shape is an E1/E3
implementation detail; the requirement is fixed here.

## Consequences

- **The UUID→`int64`-key problem dissolves.** It existed only because identity was assumed to
  be in the key path. With identity out of derivation entirely, there is nothing to encode.
  `prng.Key` stays `int64`; the tag registry and hash encoding stay frozen
  ([ADR-0001](adr-0001-counter-based-prng.md)), untouched.
- **Existing Genesis golden vectors regenerate.** Dropping `generatorID`/`version` from the
  root changes the derived streams (e.g. `Derive(TagSystem, 1, 1)` → `Derive(TagSystem)`), so
  the pinned golden JSON changes. No game persists yet, so this is free now and must land
  before the first real game (ADR-0016: appending/reshaping is free while no game exists).
- **ADR-0016 is amended, not superseded.** Its core/supplement split, per-stage versioning,
  and "a game records its generators" all stand. Only the seed-root formula changes: identity
  and version move from the derived root to recorded provenance.
- **The `home_template` / `home_template_deposit` tables (migrations 0005–0006) are
  superseded.** A home system is ordinary `planet` / `deposit` rows for the chosen `(q, r)`,
  produced by a home-system generator. Alpha data is disposable and migrations may be squashed
  (CLAUDE.md), so these tables are dropped or repurposed rather than migrated. Genesis's
  `HomeTemplate()` becomes a home-system generator, or is retired.
- **New per-system provenance surface.** The `system` record (or a new per-system table) must
  carry which generator produced its contents — defaulting to the stage generator, overridden
  on a home overwrite.
- **Founding gains a concrete mechanism.** `faction.md` §Founding is a stub; this gives it a
  mechanic — pick a system, rebuild it with a home-system generator, then assign the faction
  — without inventing observable rules (the layout the generator produces is the
  supplement's business).
- **Upstream "template" language needs reconciling.** The Genesis System Contents supplement
  describes a "home-system template." Per docs-first (CLAUDE.md rule 3), that framing should
  become "home-system generator" upstream before the engine relies on it. Draft/alpha status
  makes this a cheap follow-up, not a blocker.

## Alternatives considered

- **Keep a separate small-integer "generator slot" for the seed root** (identity stays a UUID,
  the root uses an int). Rejected: it reintroduces a second id space and still welds generator
  selection into the frozen root, for a reproducibility property the recorded selection already
  provides.
- **Keep the home-system template and copy it at founding.** Rejected: it is a special case the
  generator model already covers. A template is a frozen *output*; a generator is the thing
  that *produces* outputs — and the GM wants to choose the home-system generator the same way
  she chooses every other generator.
- **Model the home overwrite as a mid-game (timebound) mutation.** Rejected: founding is setup
  (turn 0). Contents settle at start-of-life; putting a turn axis on tables that are
  deliberately start-of-life immutable to accommodate a setup-time overwrite is the wrong
  trade.
