// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package agent

import (
	"context"
	"fmt"

	"github.com/antimetal/agent/internal/kubernetes/cluster"
	k8sv1 "github.com/antimetal/agent/pkg/api/kubernetes/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/errors"
	"github.com/antimetal/agent/pkg/resource"
	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	kindResource     = string((&resourcev1.Resource{}).ProtoReflect().Descriptor().FullName())
	kindRelationship = string((&resourcev1.Relationship{}).ProtoReflect().Descriptor().FullName())
)

type indexer struct {
	clusterName string
	provider    cluster.Provider
	store       resource.Store
}

func (i *indexer) LoadClusterInfo(ctx context.Context, major string, minor string) error {
	clusterName, err := i.provider.ClusterName(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}
	region, err := i.provider.Region(ctx)
	if err != nil {
		return fmt.Errorf("failed to get region: %w", err)
	}
	cluster := &k8sv1.Cluster{
		MajorVersion: major,
		MinorVersion: minor,
		Provider:     getProvider(i.provider),
	}
	clusterAny, err := anypb.New(cluster)
	if err != nil {
		return fmt.Errorf("failed to marshal cluster: %w", err)
	}
	i.clusterName = clusterName

	return i.store.UpdateResource(&resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: string(cluster.ProtoReflect().Descriptor().FullName()),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_KUBERNETES,
			ProviderId: clusterName,
			Name:       clusterName,
			Region:     region,
		},
		Spec: clusterAny,
	})
}

func (i *indexer) Update(ctx context.Context, obj object) error {
	rsrc, rels, err := i.generate(obj)
	if err != nil {
		return fmt.Errorf("failed to generate resource: %w", err)
	}
	if err := i.store.UpdateResource(rsrc); err != nil {
		return fmt.Errorf("failed to update resource to inventory: %w", err)
	}

	relsToAdd := make([]*resourcev1.Relationship, 0)
	for _, rel := range rels {
		pred, err := anypb.UnmarshalNew(rel.Predicate, proto.UnmarshalOptions{})
		if err != nil {
			return fmt.Errorf("failed to unmarshal predicate: %w", err)
		}
		_, err = i.store.GetRelationships(rel.GetSubject(), rel.GetObject(), pred)
		if err != nil {
			if !errors.Is(err, resource.ErrRelationshipsNotFound) {
				err = fmt.Errorf("failed to find existing relationships: %w", err)
				return errors.NewRetryable(err.Error())
			}
			relsToAdd = append(relsToAdd, rel)
		}
	}
	if len(relsToAdd) > 0 {
		if err := i.store.AddRelationships(relsToAdd...); err != nil {
			err = fmt.Errorf("failed to add relationship: %w", err)
			return errors.NewRetryable(err.Error())
		}
	}
	return nil
}

func (i *indexer) Delete(ctx context.Context, obj object) error {
	ref := &resourcev1.ResourceRef{
		TypeUrl: gogoproto.MessageName(obj),
		Name:    obj.GetName(),
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   i.clusterName,
					Namespace: obj.GetNamespace(),
				},
			},
		},
	}
	return i.store.DeleteResource(ref)
}

func getProvider(prov cluster.Provider) k8sv1.ClusterProvider {
	switch prov.Name() {
	case cluster.ProviderEKS:
		return k8sv1.ClusterProvider_CLUSTER_PROVIDER_EKS
	case cluster.ProviderGKE:
		return k8sv1.ClusterProvider_CLUSTER_PROVIDER_GKE
	case cluster.ProviderAKS:
		return k8sv1.ClusterProvider_CLUSTER_PROVIDER_AKS
	case cluster.ProviderKIND:
		return k8sv1.ClusterProvider_CLUSTER_PROVIDER_KIND
	default:
		return k8sv1.ClusterProvider_CLUSTER_PROVIDER_OTHER
	}
}
