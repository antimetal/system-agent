// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt
package agent

import (
	"context"
	"flag"

	"github.com/go-logr/logr"

	"github.com/antimetal/agent/internal/kubernetes/cluster"
)

var (
	enable   bool
	provider string

	eksAccountID    string
	eksRegion       string
	eksClusterName  string
	eksAutodiscover bool
)

func init() {
	flag.StringVar(&provider, "kubernetes-provider", "kind", "The Kubernetes provider")
	flag.BoolVar(&enable, "enable-kubernetes-controller", true,
		"Enable Kubernetes cluster snapshot collector")
	flag.StringVar(&eksAccountID, "kubernetes-provider-eks-account-id", "",
		"The AWS account ID the EKS cluster is deployed in")
	flag.StringVar(&eksRegion, "kubernetes-provider-eks-region", "",
		"The AWS region the EKS cluster is deployed in")
	flag.StringVar(&eksClusterName, "kubernetes-provider-eks-cluster-name", "",
		"The name of the EKS cluster")
	flag.BoolVar(&eksAutodiscover, "kubernetes-provider-eks-autodiscover", true,
		"Autodiscover EKS cluster name")
}

func getDefaultProvider(ctx context.Context, logger logr.Logger) (cluster.Provider, error) {
	opts := cluster.ProviderOptions{
		Logger: logger,
		EKS: cluster.EKSOptions{
			Autodiscover: eksAutodiscover,
			AccountID:    eksAccountID,
			Region:       eksRegion,
			ClusterName:  eksClusterName,
		},
	}
	return cluster.GetProvider(ctx, provider, opts)
}

func Enabled() bool { return enable }
