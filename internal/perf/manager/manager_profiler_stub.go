// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !linux

package manager

import (
	"context"
	"fmt"

	agentv1 "github.com/antimetal/agent/pkg/api/antimetal/agent/v1"
)

func (m *manager) startProfileCollector(ctx context.Context, configObj *agentv1.ProfileCollectionConfig, name string) (context.CancelFunc, error) {
	return nil, fmt.Errorf("ProfileCollectionConfig not supported on non-Linux platforms")
}
