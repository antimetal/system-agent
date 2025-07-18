// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package store

import (
	"encoding/base64"
	"fmt"
	"strings"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
)

const (
	cloudNs = "cloud"
	kubeNs  = "kube"
)

// ref creates a new ResourceRef from Resource.
func ref(r *resourcev1.Resource) *resourcev1.ResourceRef {
	return &resourcev1.ResourceRef{
		TypeUrl:   r.GetType().GetType(),
		Name:      r.GetMetadata().GetName(),
		Namespace: r.GetMetadata().GetNamespace(),
	}
}

// encode encodes the ResourceRef into string format.
func encodeResourceKey(r *resourcev1.ResourceRef) (string, error) {
	if r == nil {
		return "", fmt.Errorf("resource must not be nil")
	}

	if r.GetTypeUrl() == "" {
		return "", fmt.Errorf("missing type")
	}

	var name string

	ns := r.GetNamespace()
	if ns == nil {
		name = r.Name
	} else {
		switch ns := ns.GetNamespace().(type) {
		case *resourcev1.Namespace_Cloud:
			name = fmt.Sprintf("%s/%s/%s/%s/%s",
				cloudNs,
				ns.Cloud.GetAccount().GetAccountId(),
				ns.Cloud.GetRegion(),
				ns.Cloud.GetGroup(),
				r.Name,
			)
		case *resourcev1.Namespace_Kube:
			name = fmt.Sprintf("%s/%s/%s/%s",
				kubeNs,
				ns.Kube.GetCluster(),
				ns.Kube.GetNamespace(),
				r.Name,
			)
		default:
			name = r.Name
		}
	}
	obj := base64.URLEncoding.EncodeToString([]byte(name))
	return fmt.Sprintf("%s/%s", r.GetTypeUrl(), obj), nil
}

// decodeResourceKey decodes the ResourceRef from string format.
func decodeResourceKey(key string, r *resourcev1.ResourceRef) error {
	if r == nil {
		return fmt.Errorf("resource must not be nil")
	}

	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid key format: %s", key)
	}
	r.TypeUrl = parts[0]
	d, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("failed to decode key: %w", err)
	}

	parts = strings.Split(string(d), "/")
	if len(parts) == 1 {
		r.Name = parts[0]
		return nil
	}

	switch parts[0] {
	case cloudNs:
		if len(parts[1:]) != 4 {
			return fmt.Errorf("invalid format: %s", key)
		}
		r.Namespace = &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Cloud{
				Cloud: &resourcev1.CloudNamespace{
					Account: &resourcev1.ProviderAccount{
						AccountId: parts[1],
					},
					Region: parts[2],
					Group:  parts[3],
				},
			},
		}
		r.Name = parts[4]
	case kubeNs:
		if len(parts[1:]) != 3 {
			return fmt.Errorf("invalid format: %s", key)
		}
		r.Namespace = &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   parts[1],
					Namespace: parts[2],
				},
			},
		}
		r.Name = parts[3]
	}
	return nil
}
