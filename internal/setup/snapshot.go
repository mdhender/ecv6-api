// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package setup

import (
	"github.com/mdhender/ecv6-api/internal/prng"
	"github.com/mdhender/ecv6-api/internal/store"
)

// snapshotToDomain is the store→domain (load) adapter, the counterpart to the
// domainToStore (persist) adapter in mapping.go. It adapts a game's stored engine
// snapshot into the domain-layer input the generator consumes. At turn 0 the world
// is empty, so the snapshot is just the game's master seeds; the ClusterGenerator
// (genesis.GenesisCluster) fills an empty cluster from scratch off those seeds.
// When the store later holds a partial or in-play cluster, this is the seam where
// those rows would load into a domains.Cluster before regeneration.
func snapshotToDomain(es store.EngineState) prng.Seeds {
	return prng.New(es.Seed1, es.Seed2)
}
