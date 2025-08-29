// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import "strings"

var (
	// These variables are supplied at build time
	buildMajor string
	buildMinor string
	buildPatch string
	buildRev   string
)

// Version returns the semantic version (major.minor.patch).
func Version() string {
	return strings.Join([]string{buildMajor, buildMinor, buildPatch}, ".")
}

// Rev returns the revision id of the build the runtime is using.
func Rev() string {
	return buildRev
}
