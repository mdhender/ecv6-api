# Deposits

Contributor reference for how the engine implements **deposit generation** at
setup — *how the back end places the natural-resource deposits planets carry*, not
the player-facing rules for what those deposits are. This stage implements the
**Genesis Deposits** generator; the rules it implements (endowments, planet
shares, deposit counts, quantities, the yield ladder, the habitability penalty,
and how the abundance settings apply) live upstream and are the source of truth:

- [Genesis Deposits](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/deposits.md)
  — the generator this stage implements (draft, v1).
- [Genesis family index](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/_index.md)
  — the three staged generators and the abundance settings (`fuel`, `mtls`,
  `nmtl`) this stage reads.
- [Cluster core](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md)
  — the shared schema: three resources (fuel, metals, non-metals), each deposit a
  quantity and a yield.

Never restate the rules here; link them. This page is engine mechanism and the
stage seam. See [`doc/README.md`](../README.md).

> **Not yet implemented.** The rules are grounded upstream (CLAUDE.md rule 3) —
> this is the grounding the deposit work
> ([mdhender/ecv6-api#67](https://github.com/mdhender/ecv6-api/issues/67)) was
> blocked on, now resolved by [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)
> and the Genesis Deposits supplement (note `Af/Am/An` remain placeholders while
> Genesis is draft). The engine generator does not exist yet; implementation is
> planned and ticketed separately. This page describes how it *will* implement
> Genesis, so it stays in step as code lands.

## Deposits are stochastic and consume the abundance settings

Genesis Deposits is **fully stochastic**: system endowments scale by planet count,
planets take resource shares by affinity, deposit counts and per-deposit weights
are rolled, and quantity and yield each take an independent adjustment roll, with a
habitability penalty on yield. It **does** consume the cluster's `fuel`, `mtls`,
and `nmtl` abundance settings — each setting shifts its resource's final quantity
and yield.

This retires the earlier interim behavior — deterministic, no random draw,
quantity `⌈100M / n⌉`, a fixed yield-halving formula, and explicitly *not*
consuming the abundance settings. Do not carry any of those numbers forward; the
supplement's dice, tables, and ladders are the truth.

## The stage seam

Generation is staged and each stage's generator is chosen independently
([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
Deposits runs **last**, after system contents:

- **In:** `(planet type, orbit)` for every planet of every ordinary system, plus
  the fixed home-system template, from the system-contents stage; and the three
  abundance settings.
- **Out:** for every planet, its deposits — each exactly one resource, with a
  positive whole-number initial quantity and an initial yield in `0.1%`
  increments. The completed home-system template is copied unchanged per player
  (counts, quantities, and yields are not rerolled per player).

Because deposits consumes `(planet type, orbit)`, the system-contents output is the
compatibility surface between the two stages — see
[System generation](system-generation.md).

## Determinism

Each Genesis stage draws from its **own seed root**, derived
`Derive(stageTag, generatorID, version)`
([ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md)).
The deposit stage has **no domain tag today** — the registry holds `TagCluster`,
`TagSystem`, and `TagPlayer`. Whether deposits hang under `TagSystem` with a
distinct generator id or take an appended `TagDeposit` is an **open implementation
question for E1**; appending is free while no game exists, and the registry is
append-only. The domain-tag registry and the key-path hash encoding stay globally
frozen; a generator's internal addressing freezes per version, on its own
schedule. See [`doc/determinism.md`](../determinism.md) and `internal/prng`.

## See also

- [System generation](system-generation.md) — the prior stage, which emits the
  planet types and orbits these deposits attach to.
- [`doc/determinism.md`](../determinism.md) — streams, key paths, domain tags,
  frozen surfaces.
- [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md) —
  the core/supplement split and per-generator seed roots.
- [Genesis Deposits](https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/deposits.md)
  — the rules this stage implements.
