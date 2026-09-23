// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"reflect"
	"testing"

	"ItsBagelBot/app/deployer/internal/ports"
)

func TestSelect(t *testing.T) {
	objs := testdataObjects(t)
	cases := []struct {
		name string
		got  func(ports.Objects) []string
		want []string
	}{
		{
			name: "named order first, unnamed in manifest order, unknown names dropped",
			got: func(o ports.Objects) []string {
				return Services(o, []string{"notifications", "discord-engine", "outgress", "unknown"})
			},
			want: []string{"notifications", "discord-engine", "outgress", "console-admin", "discord-ingress", "discord-outgress"},
		},
		{
			name: "service owns its suffixed objects and CronJob",
			got:  func(o ports.Objects) []string { return refs(ForService(o, "notifications"), nil) },
			want: []string{"DopplerSecret db/notifications-env", "Deployment db/notifications",
				"PodDisruptionBudget db/notifications", "CronJob db/notifications-cleanup"},
		},
		{
			name: "ScaledObject travels with its Deployment",
			got:  func(o ports.Objects) []string { return refs(ForService(o, "outgress"), nil) },
			want: []string{"DopplerSecret app/outgress-env", "Deployment app/outgress",
				"PodDisruptionBudget app/outgress", "ScaledObject app/outgress"},
		},
		{
			name: "one file, three services: longest name wins",
			got:  func(o ports.Objects) []string { return refs(ForService(o, "discord-engine"), nil) },
			want: []string{"Deployment app/discord-engine", "PodDisruptionBudget app/discord-engine"},
		},
		{
			name: "shared keeps PriorityClasses and the file-wide secret",
			got:  func(o ports.Objects) []string { return refs(Shared(o), nil) },
			want: []string{"PriorityClass bagel-data-plane", "PriorityClass bagel-service",
				"PriorityClass bagel-operator", "PriorityClass bagel-edge", "DopplerSecret app/discord-env"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.got(objs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
