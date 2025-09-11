// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

//go:build !integration

package store

import (
	"reflect"
	"testing"

	resourcev1 "github.com/antimetal/agent/pkg/api/resource/v1"
)

type testCase struct {
	Description   string
	TypeURL       string
	Name          string
	Namespace     *resourcev1.Namespace
	EncodedResult string
}

var testCases = []testCase{
	{
		Description: "cloud-namespace",
		TypeURL:     "foo",
		Name:        "test",
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Cloud{
				Cloud: &resourcev1.CloudNamespace{
					Account: &resourcev1.ProviderAccount{
						AccountId: "123456789012",
					},
					Region: "us-east-1",
					Group:  "test-group",
				},
			},
		},
		EncodedResult: "foo/Y2xvdWQvMTIzNDU2Nzg5MDEyL3VzLWVhc3QtMS90ZXN0LWdyb3VwL3Rlc3Q=",
	},
	{
		Description: "cloud-namespace-no-group",
		TypeURL:     "foo",
		Name:        "test2",
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Cloud{
				Cloud: &resourcev1.CloudNamespace{
					Account: &resourcev1.ProviderAccount{
						AccountId: "123456789012",
					},
					Region: "us-east-1",
				},
			},
		},
		EncodedResult: "foo/Y2xvdWQvMTIzNDU2Nzg5MDEyL3VzLWVhc3QtMS8vdGVzdDI=",
	},
	{
		Description: "kube-namespace",
		TypeURL:     "bar",
		Name:        "test3",
		Namespace: &resourcev1.Namespace{
			Namespace: &resourcev1.Namespace_Kube{
				Kube: &resourcev1.KubernetesNamespace{
					Cluster:   "test-cluster",
					Namespace: "test-namespace",
				},
			},
		},
		EncodedResult: "bar/a3ViZS90ZXN0LWNsdXN0ZXIvdGVzdC1uYW1lc3BhY2UvdGVzdDM=",
	},
	{
		Description:   "no-namespace",
		TypeURL:       "baz",
		Name:          "test4",
		EncodedResult: "baz/dGVzdDQ=",
	},
}

func TestEncodeResourceKey(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.Description, func(t *testing.T) {
			r := &resourcev1.ResourceRef{
				TypeUrl:   tc.TypeURL,
				Name:      tc.Name,
				Namespace: tc.Namespace,
			}
			result, err := encodeResourceKey(r)
			if err != nil {
				t.Errorf("Failed to encode resource key: %s", err)
			}
			if result != tc.EncodedResult {
				t.Errorf("Expected %s, got %s", tc.EncodedResult, result)
			}
		})
	}
}

func TestDecodeResourceKey(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.Description, func(t *testing.T) {
			r := &resourcev1.ResourceRef{}
			if err := decodeResourceKey(tc.EncodedResult, r); err != nil {
				t.Errorf("Failed to decode resource key: %s", err)
			}

			if r.GetTypeUrl() != tc.TypeURL {
				t.Errorf("Expected TypeURL %s, got %s", tc.TypeURL, r.GetTypeUrl())
			}
			if r.Name != tc.Name {
				t.Errorf("Expected Name %s, got %s", tc.Name, r.Name)
			}
			if equal := reflect.DeepEqual(r.Namespace, tc.Namespace); !equal {
				t.Errorf("Expected Namespace %v, got %v", tc.Namespace, r.Namespace)
			}
		})
	}
}
