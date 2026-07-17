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

> **Implemented.** The generator lives in `internal/genesis` (`GenerateDeposits`,
> `DepositSettings`), backed by the `deposit` table (`store.SaveDeposits` /
> `store.GetDeposits`). The rules are grounded upstream (CLAUDE.md rule 3) in the
> Genesis Deposits supplement, resolved by
> [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md).
> The `Af/Am/An` endowments default to `4,891,250,000`; GM entry of custom values
> is future work. There is no home-system template
> ([ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md)):
> a home system's deposits are ordinary `deposit` rows for its chosen `(q, r)`,
> produced on demand at founding (E3). This page describes the engine mechanism;
> the rules stay upstream.

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

- **In:** `(planet type, orbit)` for every planet of every ordinary system, from
  the system-contents stage; and the three abundance settings.
- **Out:** for every planet, its deposits — each exactly one resource, with a
  positive whole-number initial quantity and an initial yield in `0.1%`
  increments.

Because deposits consumes `(planet type, orbit)`, the system-contents output is the
compatibility surface between the two stages — see
[System generation](system-generation.md).

## Determinism

Each Genesis stage draws from its **own seed root**, derived `Derive(stageTag)`
([ADR-0017](../decisions/adr-0017-generator-identity-and-home-system-generation.md)
amends [ADR-0016](../decisions/adr-0016-core-rulebook-and-generator-supplements.md):
generator id and version are recorded provenance, not seed inputs). Deposits root
at `Derive(TagDeposit)`. `TagDeposit` (`4`) is the appended domain tag the registry
now carries alongside `TagCluster`, `TagSystem`, and `TagPlayer`. Each ordinary
system draws its deposits from one `Roller` at `root.Roller(Key(q), Key(r))`.
Within a system the seven phases are drawn strictly in the
documented order — each phase completed system-wide before the next — addressing
planets and resources by their deterministic order, never by Go-map iteration
order. The domain-tag registry and the key-path hash encoding stay globally
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
