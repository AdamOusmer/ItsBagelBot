// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

type fileOutcome struct {
	Body string
	Kind error
}

func TestFile(t *testing.T) {
	const path = "GET /repos/o/r/contents/deploy/k8s/gossip.yaml"
	cases := []struct {
		name string
		h    http.HandlerFunc
		want fileOutcome
	}{
		{
			name: "file",
			h:    reply(http.StatusOK, `{"type":"file","encoding":"base64","content":"aW1hZ2U6IHgK"}`),
			want: fileOutcome{Body: "image: x\n"},
		},
		{
			name: "missing",
			h:    reply(http.StatusNotFound, `{"message":"Not Found"}`),
			want: fileOutcome{Kind: ports.ErrNotFound},
		},
		{
			name: "directory",
			h:    reply(http.StatusOK, `[{"type":"file","name":"a.yaml"}]`),
			want: fileOutcome{Kind: ports.ErrInvalid},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ref string
			c, _, _ := newClient(t, routes{path: func(w http.ResponseWriter, r *http.Request) {
				ref = r.URL.Query().Get("ref")
				tc.h(w, r)
			}})
			body, err := c.File(t.Context(), "deploy/k8s/gossip.yaml", "pin1")
			assert.Equal(t, []any{tc.want, "pin1"}, []any{fileOutcome{Body: string(body), Kind: kindOf(err)}, ref})
		})
	}
}

// treeRoutes serve deploy/k8s at ref pin1: two manifests, one nested, and a
// subdirectory entry that carries no bytes.
func treeRoutes(tree string) routes {
	return routes{
		"GET /repos/o/r/contents/deploy": reply(http.StatusOK, `[
			{"name":"k8s","type":"dir","sha":"t1"},{"name":"db","type":"dir","sha":"t2"}]`),
		"GET /repos/o/r/git/trees/t1": reply(http.StatusOK, tree),
		"GET /repos/o/r/git/blobs/b1": reply(http.StatusOK, "kind: Deployment\n"),
		"GET /repos/o/r/git/blobs/b2": reply(http.StatusOK, "kind: Service\n"),
	}
}

func TestTree(t *testing.T) {
	c, fake, _ := newClient(t, treeRoutes(`{"sha":"t1","truncated":false,"tree":[
		{"path":"gossip.yaml","type":"blob","sha":"b1"},
		{"path":"sub","type":"tree","sha":"t3"},
		{"path":"sub/svc.yaml","type":"blob","sha":"b2"}]}`))
	var calls []int
	var files ports.Files
	for range 2 {
		got, err := c.Tree(t.Context(), "deploy/k8s", "pin1")
		require.NoError(t, err)
		files = got
		calls = append(calls, fake.count())
	}
	assert.Equal(t, ports.Files{
		"deploy/k8s/gossip.yaml":  []byte("kind: Deployment\n"),
		"deploy/k8s/sub/svc.yaml": []byte("kind: Service\n"),
	}, files)
	// Blobs are content addressed, so the second read re-lists but never
	// re-downloads: 4 calls, then 2 more.
	assert.Equal(t, []int{4, 6}, calls)
}

func TestTreeRefusals(t *testing.T) {
	cases := map[string]struct {
		dir  ports.FilePath
		tree string
		want error
	}{
		"truncated listing": {dir: "deploy/k8s", tree: `{"sha":"t1","truncated":true,"tree":[]}`, want: errOther},
		"no such directory": {dir: "deploy/nope", tree: `{}`, want: ports.ErrNotFound},
	}
	got := map[string]error{}
	want := map[string]error{}
	for name, tc := range cases {
		c, _, _ := newClient(t, treeRoutes(tc.tree))
		_, err := c.Tree(t.Context(), tc.dir, "pin1")
		got[name], want[name] = kindOf(err), tc.want
	}
	assert.Equal(t, want, got)
}
