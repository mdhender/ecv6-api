// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package prng

// The domain-tag registry: the leading element of every key path names the
// purpose of a draw, providing domain separation so two purposes can never
// share a stream. This is the single, authoritative place tags are defined.
//
// FROZEN SURFACE — APPEND ONLY. The block starts at 1 (0 is invalid, so a
// forgotten tag is an obvious bug rather than a silent alias). Never insert or
// reorder a constant: iota would renumber every tag after it and silently
// rewrite every live game. To add a tag, append it to the END of this block and
// pin a golden vector for its stream.
const (
	_          Key = iota // 0 is invalid — never use as a domain tag
	TagCluster            // 1: cluster generation
	TagSystem             // 2: per-system contents, addressed by (q, r)
	TagPlayer             // 3: per-player draws, addressed by player_id
)
