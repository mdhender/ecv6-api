// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package genesis implements the Genesis family of cluster generators — the v1
// testbed generators the GM may choose for each stage of cluster generation.
//
// This package holds the ENGINE mechanism, not the rules. The rules — the
// settings, the radius tiers, the placement algorithm, and its failure mode —
// are published, player-facing, and are the source of truth; do not restate
// them here. Ground any behavior in the supplement and link it:
//
// Genesis Placement (stage 1):
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/placement.md
//
// Genesis System Contents (stage 2):
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/system-contents.md
//
// Genesis Deposits (stage 3):
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/generators/genesis/deposits.md
//
// The distance metric and the axial (q, r) hex schema are core rulebook:
// https://github.com/mdhender/ecv6-docs/blob/main/content/reference/cluster.md
//
// Determinism. Each Genesis stage roots its randomness at
// Derive(stageTag, generatorID, version) (ADR-0016); below that root the
// generator owns its addressing entirely. Placement roots at
// Derive(TagCluster, PlacementGeneratorID, PlacementVersion) and draws its hex
// shuffle from a single stream off that root. System Contents roots at
// Derive(TagSystem, SysContentsGeneratorID, SysContentsVersion) and draws each
// ordinary system from a per-(q, r) stream off that root. Deposits roots at
// Derive(TagDeposit, DepositsGeneratorID, DepositsVersion) and draws each system
// (and the home template, off its sentinel (q, r)) from a per-(q, r) stream in
// the documented seven-phase order. So the same seeds and the same inputs
// reproduce the same map on any machine, independent of Go-map iteration order.
// See doc/determinism.md and internal/prng.
package genesis
