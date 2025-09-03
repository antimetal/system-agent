// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config_test

import (
	"github.com/antimetal/agent/internal/config/internal/mock"
	"github.com/antimetal/agent/pkg/performance"
)

func init() {
	performance.Register("cpu", mock.NewCollector("cpu"))
	performance.Register("memory", mock.NewCollector("memory"))
}
