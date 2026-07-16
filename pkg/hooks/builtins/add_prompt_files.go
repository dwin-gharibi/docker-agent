package builtins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"

	"github.com/docker/docker-agent/pkg/hooks"
	"github.com/docker/docker-agent/pkg/promptfiles"
)

// AddPromptFiles is the registered name of the add_prompt_files builtin.
const AddPromptFiles = "add_prompt_files"

const promptFilesGroup = "core/prompt-files"

// addPromptFiles reads each filename from the workdir hierarchy and home
// directory, preserving each resolved path in independently diffable context.
func addPromptFiles(_ context.Context, in *hooks.Input, args []string) (*hooks.Output, error) {
	if in == nil || in.Cwd == "" || len(args) == 0 {
		return nil, nil
	}
	home, _ := os.UserHomeDir()
	var sources []hooks.InstructionContext
	for _, name := range args {
		for _, path := range promptfiles.PathsFromEnv(in.Cwd, home, name) {
			content, err := os.ReadFile(path)
			if err != nil {
				slog.Warn("reading prompt file", "path", path, "error", err)
				return instructionContextOutput(hooks.InstructionContext{
					Group: promptFilesGroup, Unavailable: true, SetMarker: true,
				}), nil
			}
			rendered := "Instructions from: " + path + "\n" + string(content)
			sources = append(sources, hooks.InstructionContext{
				Key:            promptFileKey(path),
				Group:          promptFilesGroup,
				Label:          "instructions from " + path,
				Content:        rendered,
				ChangedContent: "The instructions from " + path + " have changed and replace the previous instructions from that file.\n\n" + rendered,
				RemovedContent: "The previously loaded instructions from " + path + " no longer apply.",
			})
		}
	}
	if len(sources) == 0 {
		sources = append(sources, hooks.InstructionContext{
			Group: promptFilesGroup, CompleteGroup: true, SetMarker: true,
		})
	} else {
		sources[0].CompleteGroup = true
	}
	return instructionContextOutput(sources...), nil
}

func instructionContextOutput(sources ...hooks.InstructionContext) *hooks.Output {
	return &hooks.Output{HookSpecificOutput: &hooks.HookSpecificOutput{
		HookEventName:      hooks.EventTurnStart,
		InstructionContext: sources,
	}}
}

func promptFileKey(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "core/prompt-file-" + hex.EncodeToString(sum[:])
}
