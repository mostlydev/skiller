package status

import (
	"testing"

	"github.com/mostlydev/skiller/pkg/state"
)

func TestLedgerOnlyInstallReportsNotSeen(t *testing.T) {
	report := Build(Inputs{
		Ledger: state.Ledger{
			Schema: "skiller-state.v1",
			Skills: []state.SkillRecord{{
				ID:          "skill-001",
				CanonicalID: "mostlydev:talking-stick",
				Namespace:   "mostlydev",
				Name:        "talking-stick",
				InstallSlug: "talking-stick",
			}},
			Installs: []state.InstallRecord{{
				ID:         "install-001",
				SkillID:    "skill-001",
				TargetKind: "shared",
				TargetID:   "agents",
				TargetPath: "/tmp/.agents/skills/talking-stick",
				Mode:       "link",
				Scope:      "host",
				Status:     "installed",
			}},
		},
	})
	if len(report.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(report.Items))
	}
	item := report.Items[0]
	if item.Status != "not-seen" {
		t.Fatalf("status = %q, want not-seen", item.Status)
	}
	if item.Ownership.Class != "absent" {
		t.Fatalf("ownership class = %q, want absent", item.Ownership.Class)
	}
}
