// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseImageNameTag(t *testing.T) {
	tests := []struct {
		name         string
		image        string
		expectedName string
		expectedTag  string
		description  string
	}{
		{
			name:         "empty_image",
			image:        "",
			expectedName: "",
			expectedTag:  "",
			description:  "Empty image should return empty name and tag",
		},
		{
			name:         "image_without_tag",
			image:        "nginx",
			expectedName: "nginx",
			expectedTag:  "latest",
			description:  "Image without tag should default to 'latest'",
		},
		{
			name:         "image_with_tag",
			image:        "nginx:1.21",
			expectedName: "nginx",
			expectedTag:  "1.21",
			description:  "Image with tag should parse correctly",
		},
		{
			name:         "image_with_registry",
			image:        "docker.io/library/nginx:1.21",
			expectedName: "docker.io/library/nginx",
			expectedTag:  "1.21",
			description:  "Image with registry should parse correctly",
		},
		{
			name:         "image_with_registry_port",
			image:        "localhost:5000/myimage:latest",
			expectedName: "localhost:5000/myimage",
			expectedTag:  "latest",
			description:  "Image with registry port should parse correctly",
		},
		{
			name:         "kubernetes_pause_container",
			image:        "registry.k8s.io/pause:3.8",
			expectedName: "registry.k8s.io/pause",
			expectedTag:  "3.8",
			description:  "Kubernetes pause container should parse correctly",
		},
		{
			name:         "image_with_sha256",
			image:        "nginx@sha256:abcd1234",
			expectedName: "nginx@sha256:abcd1234",
			expectedTag:  "latest",
			description:  "Image with SHA256 digest should not be parsed as tag",
		},
		{
			name:         "complex_registry_path",
			image:        "registry.example.com:443/namespace/repo/image:v1.2.3",
			expectedName: "registry.example.com:443/namespace/repo/image",
			expectedTag:  "v1.2.3",
			description:  "Complex registry path should parse correctly",
		},
		{
			name:         "registry_with_path_no_tag",
			image:        "gcr.io/google-containers/pause",
			expectedName: "gcr.io/google-containers/pause",
			expectedTag:  "latest",
			description:  "Registry with path but no tag should default to latest",
		},
		{
			name:         "image_with_path_in_potential_tag",
			image:        "registry.example.com/path/with:colon/image",
			expectedName: "registry.example.com/path/with:colon/image",
			expectedTag:  "latest",
			description:  "Image with colon in path should not be parsed as tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, tag := parseImageNameTag(tt.image)
			assert.Equal(t, tt.expectedName, name, tt.description+" (name)")
			assert.Equal(t, tt.expectedTag, tag, tt.description+" (tag)")
		})
	}
}
