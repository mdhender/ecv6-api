// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package worldgen defines the mix-and-match generator contract for cluster
// setup: the GM picks a generator independently for each layer (cluster/map,
// system contents, planet detail) and the orchestrator composes them. Genesis
// (internal/genesis) becomes ONE option — a monolithic cluster generator — not
// the contract.
//
// This package holds two things and no algorithms:
//
//   - The three generator interfaces — [ClusterGenerator], [SystemGenerator],
//     [PlanetGenerator] — over a uniform ([Knobs], [prng.Seeds]) signature, plus
//     the [Generator] base ([Identity], [Scope] layers, and a [Flavor] intent
//     modifier) that every implementation carries. Scope names which container's
//     contents a generator fills; Flavor (e.g. FlavorHome) says where it is
//     offered in the GM's workflow, orthogonal to the layers.
//   - The typed knobs ([Knobs], the aggregate "MetaKnob") — well-defined game
//     values (system count, density, spacing, per-resource abundance), not
//     per-generator settings — so the signature is identical across a role and a
//     GM can save a reusable layout.
//
// The generator-facing domain model ([domains.Cluster]/[domains.System]/
// [domains.Planet]/[domains.Deposit]) lives in the behavior-free leaf package
// internal/domains, shared with genesis, setup, and the future engine; the
// interfaces here reference it.
//
// Determinism (ADR-0017): generator identity is SELECTION and PROVENANCE only,
// never entropy. The UUID and version never enter the PRNG key path. Seeds are
// derived purely from the game root and the frozen stage tags (prng.TagCluster,
// prng.TagSystem, prng.TagDeposit) plus coordinate keys; the orchestrator hands
// each call the private root already scoped to its unit of work. Reproducibility
// comes from recording the selection + Knobs, not from mixing the id into seeds.
//
//	https://github.com/mdhender/ecv6-api/blob/main/doc/decisions/adr-0017-generator-identity-and-home-system-generation.md
//	https://github.com/mdhender/ecv6-api/blob/main/doc/decisions/adr-0016-core-rulebook-and-generator-supplements.md
//
// The rules these generators implement are player-facing and published upstream;
// do not restate them here. See the Genesis supplements:
//
//	https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis
package worldgen
