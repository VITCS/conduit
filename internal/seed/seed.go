// Package seed provides the default workflow so a fresh server opens on a
// realistic canvas rather than an empty one. It mirrors the "Preview
// Environments" pipeline the Conduit UI ships with.
package seed

import "conduit/internal/model"

func f(key, icon, value string) model.Field { return model.Field{Key: key, Icon: icon, Value: value} }

// Workflow returns the default "Preview Environments" DAG.
func Workflow() model.Workflow {
	return model.Workflow{
		ID:        "wf_preview_envs",
		ProjectID: model.DefaultProjectID,
		Name:      "Preview Environments · DigitalOcean",
		Nodes: []model.Node{
			{ID: "t1", Type: "github", Icon: "github", Kind: model.KindTrigger, Title: "On Pull Request", X: 40, Y: 300,
				Config: []model.Field{f("repo", "github", "storejs / storefront"), f("on", "webhook", "pull_request.opened")}},
			{ID: "g1", Type: "timegate", Icon: "clock", Kind: model.KindStep, Title: "Business Hours Gate", X: 330, Y: 300,
				Config: []model.Field{f("window", "clock", "Mon–Fri 09–18"), f("tz", "globe", "Europe/Lisbon")}},
			{ID: "p1", Type: "http", Icon: "server", Kind: model.KindStep, Title: "Provision Server", X: 620, Y: 300,
				Config: []model.Field{f("region", "pin", "nyc1"), f("size", "size", "s-1vcpu-1gb"), f("image", "img", "ubuntu-24-04-x64")}},
			{ID: "c1", Type: "ssh", Icon: "terminal", Kind: model.KindStep, Title: "Configure App", X: 910, Y: 300,
				Config: []model.Field{f("host", "server", "159.89.230.13"), f("run", "file", "scripts/preview-setup.sh")}},
			{ID: "h1", Type: "http", Icon: "heart", Kind: model.KindStep, Title: "Health Check", X: 1200, Y: 300,
				Config: []model.Field{f("url", "globe", "http://159.89.230.13/healthz"), f("expect", "approve", "200 · 3 retries")}},
			{ID: "a1", Type: "approval", Icon: "approve", Kind: model.KindStep, Title: "Require Approval", X: 1200, Y: 560,
				Config: []model.Field{f("approvers", "user", "on-call · product"), f("timeout", "clock", "4h")}},
			{ID: "u1", Type: "http", Icon: "globe", Kind: model.KindStep, Title: "Publish Preview URL", X: 1490, Y: 560,
				Config: []model.Field{f("target", "github", "PR #154 comment"), f("url", "globe", "pr-154.preview.storejs.dev")}},
			{ID: "m1", Type: "memory", Icon: "db", Kind: model.KindStep, Title: "Record Environment", X: 1490, Y: 300,
				Config: []model.Field{f("store", "db", "preview-envs"), f("key", "key", "pr_number"), f("set", "file", "droplet_id, url, sha")}},
			{ID: "sf", Type: "status", Icon: "flag", Kind: model.KindStatus, Title: "Mark Failed", X: 910, Y: 560,
				Config: []model.Field{f("set", "github", "failure"), f("notify", "user", "author + on-call")}},
		},
		Edges: []model.Edge{
			{From: "t1", To: "g1"},
			{From: "g1", To: "p1"},
			{From: "p1", To: "c1", Label: "success"},
			{From: "c1", To: "h1", Label: "success"},
			{From: "c1", To: "sf", Label: "failed"},
			{From: "h1", To: "m1", Label: "success"},
			{From: "h1", To: "a1", Label: "success"},
			{From: "h1", To: "sf", Label: "failed"},
			{From: "a1", To: "u1", Label: "approved"},
		},
	}
}
