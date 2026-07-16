// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package ecv6_api

import (
	"github.com/maloquacious/semver"
)

var (
	version = semver.Version{
		Major:      0,
		Minor:      27,
		Patch:      0,
		PreRelease: "alpha",
		Build:      semver.Commit(),
	}
)

func Version() semver.Version {
	return version
}
