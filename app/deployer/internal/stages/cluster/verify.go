// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/app/deployer/internal/stage"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type verify struct{}

func (verify) ID() deploy.StageID { return deploy.StageVerify }

func (verify) Done(context.Context, *stage.RunCtx) (bool, error) { return false, nil }

func (verify) Run(ctx context.Context, rc *stage.RunCtx) error {
	t, err := loadTarget(ctx, rc)
	if err != nil {
		return err
	}
	units := managedUnits(t.layout.units)
	urls := probeURLs(slices.Concat(managedOnly(t.objs), t.routes))
	v := &verifyRun{rc: rc, total: len(units) + len(urls)}
	v.seed(ctx, units, urls)
	if err := v.images(ctx, units); err != nil {
		return err
	}
	v.probes(ctx, urls)
	return v.result()
}

type verifyRun struct {
	rc       *stage.RunCtx
	total    int
	checked  int
	failed   int
	problems []string
}

func (v *verifyRun) seed(ctx context.Context, units []*unit, urls []ports.URL) {
	rows := make([]deploy.Item, 0, v.total)
	for _, u := range units {
		rows = append(rows, deploy.Item{Key: imageKey(u), Label: u.name, State: deploy.StatePending})
	}
	for _, url := range urls {
		rows = append(rows, deploy.Item{Key: string(url), Label: string(url), State: deploy.StatePending})
	}
	v.rc.SetItems(ctx, rows)
}

func imageKey(u *unit) string { return "image:" + u.name }

func (v *verifyRun) images(ctx context.Context, units []*unit) error {
	repo := v.rc.Deps.Config.ImageRepo
	for _, u := range units {
		bad, err := v.rc.Deps.Watcher.VerifyImageIDs(ctx, u.refs(), u.pins(repo))
		if err != nil {
			return ports.Failf(deploy.FailKube, "read %s pods: %v", u.name, err)
		}
		v.record(ctx, imageResult(u, bad))
	}
	return nil
}

type check struct {
	row      deploy.Item
	problems []string
}

func imageResult(u *unit, bad []ports.Mismatch) check {
	row := deploy.Item{Key: imageKey(u), Label: u.name, State: deploy.StateSucceeded}
	if len(bad) == 0 {
		return check{row: row}
	}
	row.State, row.Detail = deploy.StateFailed, fmt.Sprintf("%d container(s) not on the pinned digest", len(bad))
	lines := make([]string, len(bad))
	for i, m := range bad {
		lines[i] = fmt.Sprintf("%s/%s pod %s container %s runs %s, pinned %s",
			m.Workload.Namespace, m.Workload.Name, m.Pod, m.Container, m.Got, m.Want)
	}
	return check{row: row, problems: lines}
}

func (v *verifyRun) probes(ctx context.Context, urls []ports.URL) {
	for _, url := range urls {
		code, err := v.rc.Deps.Watcher.Probe(ctx, url)
		v.record(ctx, probeResult(url, code, err))
	}
}

func probeResult(url ports.URL, code int, err error) check {
	row := deploy.Item{Key: string(url), Label: string(url), State: deploy.StateSucceeded, Detail: strconv.Itoa(code)}
	problem := probeProblem(code, err)
	if problem == "" {
		return check{row: row}
	}
	row.State, row.Detail = deploy.StateFailed, problem
	return check{row: row, problems: []string{string(url) + ": " + problem}}
}

func probeProblem(code int, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case code < 200 || code >= 400:
		return fmt.Sprintf("answered %d", code)
	}
	return ""
}

func (v *verifyRun) record(ctx context.Context, c check) {
	v.checked++
	if c.row.State == deploy.StateFailed {
		v.failed++
	}
	v.problems = append(v.problems, c.problems...)
	v.rc.SetItem(ctx, c.row)
	v.rc.SetProgress(ctx, deploy.Progress{Done: v.checked, Total: v.total})
}

func (v *verifyRun) result() error {
	if v.failed == 0 {
		return nil
	}
	f := ports.Failf(deploy.FailVerify, "%d of %d verify checks failed; the release is deployed", v.failed, v.total)
	f.LogTail = v.problems
	f.Actions = []deploy.Action{deploy.ActionRollback}
	return f
}

var (
	hostRule = regexp.MustCompile("Host\\(`([^`]+)`\\)")
	pathRule = regexp.MustCompile("(?:Path|PathPrefix)\\(`([^`]+)`\\)")
)

func probeURLs(objs ports.Objects) []ports.URL {
	seen := map[string]bool{}
	var urls []ports.URL
	for _, t := range probeTargets(objs) {
		if !seen[t.host] {
			seen[t.host] = true
			urls = append(urls, ports.URL("https://"+t.host+t.path))
		}
	}
	return urls
}

type probeTarget struct{ host, path string }

func probeTargets(objs ports.Objects) []probeTarget {
	var out []probeTarget
	for _, o := range objs {
		for _, match := range matchesOf(o) {
			out = append(out, targetsIn(match)...)
		}
	}
	return out
}

func matchesOf(o *unstructured.Unstructured) []string {
	if o.GetKind() != kindIngressRoute {
		return nil
	}
	routes, _, _ := unstructured.NestedSlice(o.Object, "spec", "routes")
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		m, _ := r.(map[string]any)
		match, _, _ := unstructured.NestedString(m, "match")
		out = append(out, match)
	}
	return out
}

func targetsIn(match string) []probeTarget {
	path := "/"
	if m := pathRule.FindStringSubmatch(match); m != nil {
		path = m[1]
	}
	hosts := hostRule.FindAllStringSubmatch(match, -1)
	out := make([]probeTarget, len(hosts))
	for i, h := range hosts {
		out[i] = probeTarget{host: h[1], path: path}
	}
	return out
}
