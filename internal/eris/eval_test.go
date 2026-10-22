package eris

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Opt-in: use the actual production parser on raw synthetic model outputs.
// Ordinary CI remains offline; this test never calls a model or media service.
func TestEvaluationReports(t *testing.T) {
	directory := os.Getenv("ERIS_EVAL_REPORTS_DIR")
	if directory == "" {
		t.Skip("set ERIS_EVAL_REPORTS_DIR to check generated evaluation reports")
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("no evaluation reports")
	}
	for _, file := range files {
		var report struct {
			Results []struct {
				ID       string
				Raw      *string
				Messages []Message
			}
		}
		body, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(body, &report) != nil || len(report.Results) == 0 {
			t.Fatalf("invalid report %s", filepath.Base(file))
		}
		for _, row := range report.Results {
			t.Run(filepath.Base(file)+"/"+row.ID, func(t *testing.T) {
				if row.Raw == nil {
					t.Fatal("model did not complete this case")
				}
				plan, err := ParsePlan(*row.Raw)
				if err != nil {
					t.Fatal("model output rejected by production parser")
				}
				if plan.Action == "prepare" && !requestTitleMentioned(plan.Query, row.Messages) {
					t.Error("prepared title is not grounded in the conversation")
				}
			})
		}
	}
}
