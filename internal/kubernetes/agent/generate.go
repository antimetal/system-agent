// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package agent

import (
	"fmt"

	"github.com/antimetal/agent/internal/kubernetes/scheme"
	k8sv1 "github.com/antimetal/agent/pkg/api/kubernetes/v1"
	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
	"github.com/antimetal/agent/pkg/errors"
	"github.com/antimetal/agent/pkg/resource"
	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (i *indexer) generate(obj object) (rsrc *resourcev1.Resource, rels []*resourcev1.Relationship, err error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}
	var owners []object
	if obj.GetOwnerReferences() != nil {
		owners = make([]object, len(obj.GetOwnerReferences()))
		for idx, ownerRef := range obj.GetOwnerReferences() {
			ownerGvk := schema.FromAPIVersionAndKind(ownerRef.APIVersion, ownerRef.Kind)
			ownerRuntimeObj, err := scheme.Get().New(ownerGvk)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create runtime object for %s: %w", ownerGvk.String(), err)
			}
			ownerObj, ok := ownerRuntimeObj.(object)
			if !ok {
				return nil, nil, fmt.Errorf("object is not a Kubernetes object: %T", ownerRuntimeObj)
			}
			ownerObj.SetName(ownerRef.Name)
			ownerObj.SetNamespace(obj.GetNamespace())
			ownerObj.SetUID(ownerRef.UID)
			owners[idx] = ownerObj
		}
	}

	switch obj := obj.(type) {
	case *corev1.Pod:
		rsrc, rels, err = genPod(i.store, i.clusterName, obj, owners...)
	case *corev1.Node:
		rsrc, rels, err = genNode(i.clusterName, obj, owners...)
	case *corev1.PersistentVolume:
		rsrc, rels, err = genPersistentVolume(i.clusterName, obj, owners...)
	case *corev1.PersistentVolumeClaim:
		rsrc, rels, err = genPersistentVolumeClaim(i.clusterName, obj, owners...)
	case *corev1.Service:
		rsrc, rels, err = genService(i.clusterName, obj, owners...)
	case *appsv1.DaemonSet:
		rsrc, rels, err = genDaemonSet(i.clusterName, obj, owners...)
	case *appsv1.Deployment:
		rsrc, rels, err = genDeployment(i.clusterName, obj, owners...)
	case *appsv1.ReplicaSet:
		rsrc, rels, err = genReplicaSet(i.clusterName, obj, owners...)
	case *appsv1.StatefulSet:
		rsrc, rels, err = genStatefulSet(i.clusterName, obj, owners...)
	case *batchv1.Job:
		rsrc, rels, err = genJob(i.clusterName, obj, owners...)
	default:
		err = fmt.Errorf(
			"no generator found for %s %s/%s", obj.GetObjectKind().GroupVersionKind().String(),
			obj.GetNamespace(), obj.GetName(),
		)
	}

	return
}

func genPod(store resource.Store, clusterName string, obj object, owners ...object,
) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	podObj, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, nil, fmt.Errorf("object is not a Pod; got %s", obj.GetObjectKind().GroupVersionKind().String())
	}

	rsrc, rels, err := genBase(clusterName, obj, owners...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource and base relationships: %w", err)
	}

	objRef := &resourcev1.ResourceRef{
		TypeUrl:   gogoproto.MessageName(obj),
		Name:      rsrc.GetMetadata().GetName(),
		Namespace: rsrc.GetMetadata().GetNamespace(),
	}

	if podObj.Spec.NodeName != "" {
		nodeRsrc, err := store.GetResource(&resourcev1.ResourceRef{
			TypeUrl: gogoproto.MessageName(&corev1.Node{}),
			Name:    podObj.Spec.NodeName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster: clusterName,
					},
				},
			},
		})
		if err != nil {
			err = fmt.Errorf("failed to get node resource: %w", err)
			return nil, nil, errors.NewRetryable(err.Error())
		}
		rsrc.GetMetadata().Region = nodeRsrc.GetMetadata().Region
		rsrc.GetMetadata().Zone = nodeRsrc.GetMetadata().Zone

		nodeRef := &resourcev1.ResourceRef{
			TypeUrl:   nodeRsrc.GetType().GetType(),
			Name:      nodeRsrc.GetMetadata().GetName(),
			Namespace: nodeRsrc.GetMetadata().GetNamespace(),
		}

		contains := &k8sv1.Contains{}
		containsAny, err := anypb.New(contains)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
		}
		containedBy := &k8sv1.ContainedBy{}
		containedByAny, err := anypb.New(containedBy)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
		}

		rels = append(rels,
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(contains.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   nodeRef,
				Object:    objRef,
				Predicate: containsAny,
			},
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(containedBy.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   objRef,
				Object:    nodeRef,
				Predicate: containedByAny,
			},
		)
	}

	for _, volume := range podObj.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			pvcRef := &resourcev1.ResourceRef{
				TypeUrl: gogoproto.MessageName(&corev1.PersistentVolumeClaim{}),
				Name:    volume.PersistentVolumeClaim.ClaimName,
				Namespace: &resourcev1.Namespace{
					Namespace: &resourcev1.Namespace_Kube{
						Kube: &resourcev1.KubernetesNamespace{
							Cluster:   clusterName,
							Namespace: podObj.GetNamespace(),
						},
					},
				},
			}
			volumeMount := &k8sv1.VolumeMount{}
			volumeMountAny, err := anypb.New(volumeMount)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
			}
			attachedTo := &k8sv1.AttachedTo{}
			attachedToAny, err := anypb.New(attachedTo)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
			}
			rels = append(rels,
				&resourcev1.Relationship{
					Type: &resourcev1.TypeDescriptor{
						Kind: kindRelationship,
						Type: string(volumeMount.ProtoReflect().Descriptor().FullName()),
					},
					Subject:   pvcRef,
					Object:    objRef,
					Predicate: attachedToAny,
				},
				&resourcev1.Relationship{
					Type: &resourcev1.TypeDescriptor{
						Kind: kindRelationship,
						Type: string(attachedTo.ProtoReflect().Descriptor().FullName()),
					},
					Subject:   objRef,
					Object:    pvcRef,
					Predicate: volumeMountAny,
				},
			)
		}
	}

	return rsrc, rels, nil
}

func genNode(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	rsrc, rels, err := genBase(clusterName, obj, owners...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource and base relationships: %w", err)
	}
	rsrc.GetMetadata().Region = obj.GetLabels()["topology.kubernetes.io/region"]
	rsrc.GetMetadata().Zone = obj.GetLabels()["topology.kubernetes.io/zone"]
	return rsrc, rels, nil
}

func genPersistentVolume(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	rsrc, rels, err := genBase(clusterName, obj, owners...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource and base relationships: %w", err)
	}
	rsrc.GetMetadata().Region = obj.GetLabels()["topology.kubernetes.io/region"]
	rsrc.GetMetadata().Zone = obj.GetLabels()["topology.kubernetes.io/zone"]
	return rsrc, rels, nil
}

func genPersistentVolumeClaim(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	pvcObj, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return nil, nil, fmt.Errorf("object is not a PersistentVolumeClaim; got %s", obj.GetObjectKind().GroupVersionKind().String())
	}

	rsrc, rels, err := genBase(clusterName, obj, owners...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource and base relationships: %w", err)
	}

	if pvcObj.Spec.VolumeName != "" {
		objRef := &resourcev1.ResourceRef{
			TypeUrl:   gogoproto.MessageName(obj),
			Name:      rsrc.GetMetadata().GetName(),
			Namespace: rsrc.GetMetadata().GetNamespace(),
		}
		pvRef := &resourcev1.ResourceRef{
			TypeUrl: gogoproto.MessageName(&corev1.PersistentVolume{}),
			Name:    pvcObj.Spec.VolumeName,
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster: clusterName,
					},
				},
			},
		}
		boundBy := &k8sv1.BoundBy{}
		boundByAny, err := anypb.New(boundBy)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
		}
		claimsFrom := &k8sv1.ClaimsFrom{}
		claimsFromAny, err := anypb.New(claimsFrom)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
		}
		rels = append(rels,
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(claimsFrom.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   objRef,
				Object:    pvRef,
				Predicate: claimsFromAny,
			},
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(boundBy.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   pvRef,
				Object:    objRef,
				Predicate: boundByAny,
			},
		)
	}

	return rsrc, rels, nil
}

func genService(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genDaemonSet(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genDeployment(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genReplicaSet(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genStatefulSet(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genJob(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	return genBase(clusterName, obj, owners...)
}

func genBase(clusterName string, obj object, owners ...object) (*resourcev1.Resource, []*resourcev1.Relationship, error) {
	data, err := obj.Marshal()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal object: %w", err)
	}

	rsrc := &resourcev1.Resource{
		Type: &resourcev1.TypeDescriptor{
			Kind: kindResource,
			Type: gogoproto.MessageName(obj),
		},
		Metadata: &resourcev1.ResourceMeta{
			Provider:   resourcev1.Provider_PROVIDER_KUBERNETES,
			ProviderId: string(obj.GetUID()),
			Name:       obj.GetName(),
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   clusterName,
						Namespace: obj.GetNamespace(),
					},
				},
			},
			Tags: labelsToTags(obj.GetLabels()),
		},
		Spec: &anypb.Any{
			TypeUrl: gogoproto.MessageName(obj),
			Value:   data,
		},
	}

	// Add relationships to the cluster and the object.
	clusterRef := &resourcev1.ResourceRef{
		TypeUrl: string((&k8sv1.Cluster{}).ProtoReflect().Descriptor().FullName()),
		Name:    clusterName,
	}
	objRef := &resourcev1.ResourceRef{
		TypeUrl:   rsrc.Type.Type,
		Name:      rsrc.Metadata.Name,
		Namespace: rsrc.Metadata.Namespace,
	}
	rels := make([]*resourcev1.Relationship, 0, len(owners)+2)
	contains := &k8sv1.Contains{}
	containsAny, err := anypb.New(contains)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
	}
	containedBy := &k8sv1.ContainedBy{}
	containedByAny, err := anypb.New(containedBy)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create predicate: %w", err)
	}
	rels = append(rels,
		&resourcev1.Relationship{
			Type: &resourcev1.TypeDescriptor{
				Kind: kindRelationship,
				Type: string(contains.ProtoReflect().Descriptor().FullName()),
			},
			Subject:   clusterRef,
			Object:    objRef,
			Predicate: containsAny,
		},
		&resourcev1.Relationship{
			Type: &resourcev1.TypeDescriptor{
				Kind: kindRelationship,
				Type: string(containedBy.ProtoReflect().Descriptor().FullName()),
			},
			Subject:   objRef,
			Object:    clusterRef,
			Predicate: containedByAny,
		},
	)

	// Add relationships to the resource owners if any.
	for _, owner := range owners {
		ownerRef := &resourcev1.ResourceRef{
			TypeUrl: gogoproto.MessageName(owner),
			Name:    owner.GetName(),
			Namespace: &resourcev1.Namespace{
				Namespace: &resourcev1.Namespace_Kube{
					Kube: &resourcev1.KubernetesNamespace{
						Cluster:   clusterName,
						Namespace: owner.GetNamespace(),
					},
				},
			},
		}
		owns := &k8sv1.Owns{}
		ownsAny, err := anypb.New(owns)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create owns predicate: %w", err)
		}
		ownedBy := &k8sv1.OwnedBy{}
		ownedByAny, err := anypb.New(ownedBy)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create ownedBy predicate: %w", err)
		}
		rels = append(rels,
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(owns.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   ownerRef,
				Object:    objRef,
				Predicate: ownsAny,
			},
			&resourcev1.Relationship{
				Type: &resourcev1.TypeDescriptor{
					Kind: kindRelationship,
					Type: string(ownedBy.ProtoReflect().Descriptor().FullName()),
				},
				Subject:   objRef,
				Object:    ownerRef,
				Predicate: ownedByAny,
			},
		)
	}

	return rsrc, rels, nil
}

func labelsToTags(labels map[string]string) []*resourcev1.Tag {
	tags := make([]*resourcev1.Tag, 0, len(labels))
	for k, v := range labels {
		tags = append(tags, &resourcev1.Tag{
			Key:   k,
			Value: v,
		})
	}
	return tags
}
