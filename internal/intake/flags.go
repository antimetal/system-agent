// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package intake

import "flag"

var (
	defaultAPIKey string
)

func init() {
	flag.StringVar(&defaultAPIKey, "intake-api-key", "",
		"The API key to use upload resources",
	)
}
