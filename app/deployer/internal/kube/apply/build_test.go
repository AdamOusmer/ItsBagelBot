// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"maps"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestBuild(t *testing.T) {
	testdata := os.DirFS(".")
	cases := []struct {
		name    string
		root    ports.FilePath
		kinds   kindSet
		extra   ports.Files
		want    []string
		wantErr error
	}{
		{
			name:  "kustomization keeps manifest order",
			root:  "testdata/k8s",
			kinds: kindSet{"PriorityClass": true, "Deployment": true},
			want: []string{
				"PriorityClass bagel-data-plane", "PriorityClass bagel-service",
				"PriorityClass bagel-operator", "PriorityClass bagel-edge",
				"Deployment app/console-admin", "Deployment app/outgress", "Deployment db/notifications",
				"Deployment app/discord-ingress", "Deployment app/discord-engine", "Deployment app/discord-outgress",
			},
		},
		{
			name:  "generator picks up the namespace transformer",
			root:  "testdata/messaging",
			kinds: kindSet{},
			want:  []string{"ConfigMap messaging/nats-config"},
		},
		{
			name:  "root without kustomization builds its yaml files",
			root:  "testdata/db",
			kinds: kindSet{"IngressRoute": true, "Service": true},
			want:  []string{"Service db/projector-status", "IngressRoute db/service-status"},
		},
		{
			name:  "plain manifests skip subdirectories",
			root:  "testdata/db",
			kinds: kindSet{"IngressRoute": true, "Service": true},
			extra: ports.Files{"testdata/db/nested/broken.yaml": []byte("not: [valid")},
			want:  []string{"Service db/projector-status", "IngressRoute db/service-status"},
		},
		{
			name:    "root with nothing to build",
			root:    "testdata/none",
			wantErr: ports.ErrInvalid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := ports.Files{}
			if tc.wantErr == nil {
				files = readTree(t, testdata, tc.root)
			}
			maps.Copy(files, tc.extra)
			objs, err := new(Applier).Build(t.Context(), ports.BuildSpec{Files: files, Root: tc.root})
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, refs(objs, tc.kinds))
		})
	}
}

func TestRepoManifests(t *testing.T) {
	repo := os.DirFS("../../../../..")
	for _, root := range []ports.FilePath{"deploy/k8s", "deploy/messaging", "deploy/db"} {
		t.Run(string(root), func(t *testing.T) {
			assert.Empty(t, new(Applier).Lint(buildDir(t, repo, root)))
		})
	}
}
