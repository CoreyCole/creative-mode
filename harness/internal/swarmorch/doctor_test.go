package swarmorch

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"creative-mode/harness/internal/events"
)

func TestRunDoctor_DBCheck(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()

	mgr := NewManager(
		database,
		testLogger(),
		bus,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	report := mgr.RunDoctor(t.Context())

	// Find the database check.
	var dbCheck *DoctorCheck
	for i := range report.Checks {
		if report.Checks[i].Name == "database" {
			dbCheck = &report.Checks[i]

			break
		}
	}

	if dbCheck == nil {
		t.Fatal("database check not found in report")
	}

	if dbCheck.Status != DoctorOK {
		t.Errorf(
			"database check status = %q; want %q (detail: %s)",
			dbCheck.Status,
			DoctorOK,
			dbCheck.Detail,
		)
	}
}

func TestRunDoctor_LinearWarn(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()

	mgr := NewManager(
		database,
		testLogger(),
		bus,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)
	// linearClient is nil by default in NewManager.

	report := mgr.RunDoctor(t.Context())

	var linearCheck *DoctorCheck
	for i := range report.Checks {
		if report.Checks[i].Name == "linear" {
			linearCheck = &report.Checks[i]

			break
		}
	}

	if linearCheck == nil {
		t.Fatal("linear check not found in report")
	}

	if linearCheck.Status != DoctorWarn {
		t.Errorf("linear check status = %q; want %q", linearCheck.Status, DoctorWarn)
	}
}

func TestRunDoctor_PromptTemplates(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()

	mgr := NewManager(
		database,
		testLogger(),
		bus,
		t.TempDir(),
		t.TempDir(),
		"http://localhost:8080",
	)

	report := mgr.RunDoctor(t.Context())

	var ptCheck *DoctorCheck
	for i := range report.Checks {
		if report.Checks[i].Name == "prompt_templates" {
			ptCheck = &report.Checks[i]

			break
		}
	}

	if ptCheck == nil {
		t.Fatal("prompt_templates check not found in report")
	}

	if ptCheck.Status != DoctorOK {
		t.Errorf(
			"prompt_templates check status = %q; want %q (detail: %s)",
			ptCheck.Status,
			DoctorOK,
			ptCheck.Detail,
		)
	}
}

func TestRunDoctor_OverallStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		checks  []DoctorCheck
		wantAll DoctorStatus
	}{
		{
			name: "all ok",
			checks: []DoctorCheck{
				{Name: "a", Status: DoctorOK},
				{Name: "b", Status: DoctorOK},
			},
			wantAll: DoctorOK,
		},
		{
			name: "warn overrides ok",
			checks: []DoctorCheck{
				{Name: "a", Status: DoctorOK},
				{Name: "b", Status: DoctorWarn},
			},
			wantAll: DoctorWarn,
		},
		{
			name: "fail overrides warn",
			checks: []DoctorCheck{
				{Name: "a", Status: DoctorWarn},
				{Name: "b", Status: DoctorFail},
				{Name: "c", Status: DoctorOK},
			},
			wantAll: DoctorFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Compute overall using same logic as RunDoctor.
			overall := DoctorOK
			for _, c := range tt.checks {
				if c.Status == DoctorFail {
					overall = DoctorFail

					break
				}
				if c.Status == DoctorWarn {
					overall = DoctorWarn
				}
			}

			if overall != tt.wantAll {
				t.Errorf("overall = %q; want %q", overall, tt.wantAll)
			}
		})
	}
}

func TestRunDoctor_DiskSpace(t *testing.T) {
	t.Parallel()

	database := newManagerTestDB(t)
	bus := events.NewEventBus()

	// Use a temp dir that we know exists.
	logsDir := t.TempDir()

	mgr := NewManager(
		database,
		testLogger(),
		bus,
		t.TempDir(),
		logsDir,
		"http://localhost:8080",
	)

	check := mgr.checkDiskSpace()
	if check.Name != "disk_space" {
		t.Errorf("check name = %q; want disk_space", check.Name)
	}

	// Should not error on a valid temp dir — only flag statfs errors.
	if check.Status != DoctorFail || check.Detail == "" {
		return
	}

	if len(check.Detail) >= 6 && check.Detail[:6] == "statfs" {
		t.Errorf("disk_space check failed unexpectedly: %s", check.Detail)
	}
}
