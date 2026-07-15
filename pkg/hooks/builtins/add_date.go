package builtins

import (
	"context"
	"time"

	"github.com/docker/docker-agent/pkg/hooks"
)

// AddDate is the registered name of the add_date builtin.
const AddDate = "add_date"

// addDate emits today's date as turn_start additional context.
func addDate(_ context.Context, _ *hooks.Input, _ []string) (*hooks.Output, error) {
	date := time.Now().Format("2006-01-02")
	return hooks.NewInstructionContextOutput(hooks.EventTurnStart, hooks.InstructionContext{
		Key:            "core/date",
		Label:          "date",
		Content:        "Today's date: " + date,
		ChangedContent: "Today's date is now: " + date,
	}), nil
}
