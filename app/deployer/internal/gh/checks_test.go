// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gh

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	unitTests = "Run Unit Tests"
	publish   = "Publish manifest"
	pages     = "Cloudflare Pages: itsbagelbot"
)

// codesceneRule is main's ruleset as the API answered it on 2026-09-23.
var codesceneRule = reply(http.StatusOK, `[{"type":"required_status_checks","parameters":{
	"strict_required_status_checks_policy":true,
	"required_status_checks":[{"context":"`+codescene+`"}]}}]`)

type ghRun struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion,omitempty"`
	Output     *ghOutput `json:"output,omitempty"`
}

type ghOutput struct {
	Summary string `json:"summary"`
}

type ghStatus struct {
	Context string `json:"context"`
	State   string `json:"state"`
}

var (
	codesceneGreen = ghRun{Name: codescene, Status: "completed", Conclusion: "success"}
	codesceneRed   = ghRun{Name: codescene, Status: "completed", Conclusion: "failure"}
	unitGreen      = ghRun{Name: unitTests, Status: "completed", Conclusion: "success"}
	unitRed        = ghRun{Name: unitTests, Status: "completed", Conclusion: "failure"}
	unitQueued     = ghRun{Name: unitTests, Status: "queued"}
	publishSkipped = ghRun{Name: publish, Status: "completed", Conclusion: "skipped"}
)

// checksFixture is what GitHub holds for sha "abc".
type checksFixture struct {
	PRHead   bool
	Rules    http.HandlerFunc // nil: the rules endpoint must not be called
	Runs     []ghRun
	Statuses []ghStatus
}

func (f checksFixture) routes() routes {
	runs, _ := json.Marshal(map[string]any{"total_count": len(f.Runs), "check_runs": f.Runs})
	statuses, _ := json.Marshal(map[string]any{"statuses": f.Statuses})
	pulls := `[{"number":3,"state":"closed","merged_at":"2026-09-20T00:00:00Z","head":{"sha":"feature"}}]`
	if f.PRHead {
		pulls = `[{"number":4,"state":"open","head":{"sha":"abc"}}]`
	}
	rt := routes{
		"GET /repos/o/r/commits/abc/pulls":      reply(http.StatusOK, pulls),
		"GET /repos/o/r/commits/abc/check-runs": reply(http.StatusOK, string(runs)),
		"GET /repos/o/r/commits/abc/status":     reply(http.StatusOK, string(statuses)),
	}
	if f.Rules != nil {
		rt["GET /repos/o/r/rules/branches/main"] = f.Rules
	}
	return rt
}

type checksOutcome struct {
	State     deploy.CheckState
	CodeScene deploy.CheckState
	Required  []string
}

func observeChecks(sum ports.CheckSummary) checksOutcome {
	out := checksOutcome{State: sum.State, CodeScene: sum.CodeScene}
	for _, ch := range sum.Checks {
		if ch.Required {
			out.Required = append(out.Required, ch.Name)
		}
	}
	return out
}

func TestChecks(t *testing.T) {
	cases := []struct {
		name string
		fx   checksFixture
		want checksOutcome
	}{
		{
			name: "pr head gates on the ruleset only",
			fx:   checksFixture{PRHead: true, Rules: codesceneRule, Runs: []ghRun{codesceneGreen, unitRed}},
			want: checksOutcome{deploy.ChecksSuccess, deploy.ChecksSuccess, []string{codescene}},
		},
		{
			name: "pr head waits for codescene to report",
			fx:   checksFixture{PRHead: true, Rules: codesceneRule, Runs: []ghRun{unitGreen}},
			want: checksOutcome{deploy.ChecksPending, deploy.ChecksNone, nil},
		},
		{
			name: "pr head fails on red codescene",
			fx:   checksFixture{PRHead: true, Rules: codesceneRule, Runs: []ghRun{codesceneRed, unitGreen}},
			want: checksOutcome{deploy.ChecksFailure, deploy.ChecksFailure, []string{codescene}},
		},
		{
			name: "pr head without rulesets gates every check that ran",
			fx: checksFixture{PRHead: true, Rules: reply(http.StatusOK, `[]`),
				Runs: []ghRun{codesceneGreen, unitRed, publishSkipped}},
			want: checksOutcome{deploy.ChecksFailure, deploy.ChecksSuccess, []string{codescene, unitTests}},
		},
		{
			name: "pr head with rulesets hidden falls back to every check",
			fx: checksFixture{PRHead: true, Rules: reply(http.StatusForbidden, `{"message":"Resource not accessible by integration"}`),
				Runs: []ghRun{codesceneGreen, unitQueued}},
			want: checksOutcome{deploy.ChecksPending, deploy.ChecksSuccess, []string{codescene, unitTests}},
		},
		{
			name: "main commit ignores the ruleset",
			fx:   checksFixture{Runs: []ghRun{unitGreen, publishSkipped}},
			want: checksOutcome{deploy.ChecksSuccess, deploy.ChecksNone, []string{unitTests}},
		},
		{
			name: "main commit fails on a red status",
			fx:   checksFixture{Runs: []ghRun{unitGreen}, Statuses: []ghStatus{{Context: pages, State: "failure"}}},
			want: checksOutcome{deploy.ChecksFailure, deploy.ChecksNone, []string{unitTests, pages}},
		},
		{
			name: "main commit waits on a pending status",
			fx:   checksFixture{Runs: []ghRun{unitGreen}, Statuses: []ghStatus{{Context: pages, State: "pending"}}},
			want: checksOutcome{deploy.ChecksPending, deploy.ChecksNone, []string{unitTests, pages}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newClient(t, tc.fx.routes())
			sum, err := c.Checks(t.Context(), "abc")
			require.NoError(t, err)
			assert.Equal(t, tc.want, observeChecks(sum))
		})
	}
}

// TestChecksSummary pins that only a red run's summary is carried: a green
// CodeScene report must never reach a failure's log tail.
func TestChecksSummary(t *testing.T) {
	red := codesceneRed
	red.Output = &ghOutput{Summary: "Bumpy Road: merge.go run"}
	green := unitGreen
	green.Output = &ghOutput{Summary: "all 412 tests passed"}
	c, _, _ := newClient(t, checksFixture{PRHead: true, Rules: codesceneRule, Runs: []ghRun{red, green}}.routes())
	sum, err := c.Checks(t.Context(), "abc")
	require.NoError(t, err)
	got := map[string]string{}
	for _, ch := range sum.Checks {
		got[ch.Name] = ch.Summary
	}
	assert.Equal(t, map[string]string{codescene: "Bumpy Road: merge.go run", unitTests: ""}, got)
}

// TestChecksCached pins the 10 s window: a repeat inside it costs no call,
// and one at the window's end reads GitHub again.
func TestChecksCached(t *testing.T) {
	c, fake, clk := newClient(t, checksFixture{Runs: []ghRun{unitGreen}}.routes())
	var calls []int
	for _, step := range []int{0, 9, 1} {
		clk.at = clk.at.Add(secs(step))
		_, err := c.Checks(t.Context(), "abc")
		require.NoError(t, err)
		calls = append(calls, fake.count())
	}
	assert.Equal(t, []int{3, 3, 6}, calls)
}
