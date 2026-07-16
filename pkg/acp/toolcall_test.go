package acp

import (
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/todo"
)

func TestDetermineToolKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolName    string
		annotations tools.ToolAnnotations
		want        acpsdk.ToolKind
	}{
		{name: "read only hint wins", toolName: "delete_everything", annotations: tools.ToolAnnotations{ReadOnlyHint: true}, want: acpsdk.ToolKindRead},
		{name: "read only hint wins over destructive", toolName: "shell", annotations: tools.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(true)}, want: acpsdk.ToolKindRead},
		{name: "destructive hint", toolName: "shell", annotations: tools.ToolAnnotations{DestructiveHint: new(true)}, want: acpsdk.ToolKindDelete},
		{name: "destructive hint false falls through", toolName: "shell", annotations: tools.ToolAnnotations{DestructiveHint: new(false)}, want: acpsdk.ToolKindExecute},
		{name: "read_ prefix", toolName: "read_file", want: acpsdk.ToolKindRead},
		{name: "get_ prefix", toolName: "get_issue", want: acpsdk.ToolKindRead},
		{name: "list_ prefix", toolName: "list_directory", want: acpsdk.ToolKindRead},
		{name: "directory_tree", toolName: "directory_tree", want: acpsdk.ToolKindRead},
		{name: "edit_ prefix", toolName: "edit_file", want: acpsdk.ToolKindEdit},
		{name: "write_ prefix", toolName: "write_file", want: acpsdk.ToolKindEdit},
		{name: "update_ prefix", toolName: "update_todos", want: acpsdk.ToolKindEdit},
		{name: "create_ prefix", toolName: "create_directory", want: acpsdk.ToolKindEdit},
		{name: "add_ prefix", toolName: "add_comment", want: acpsdk.ToolKindEdit},
		{name: "delete_ prefix", toolName: "delete_file", want: acpsdk.ToolKindDelete},
		{name: "remove_ prefix", toolName: "remove_directory", want: acpsdk.ToolKindDelete},
		{name: "stop_ prefix", toolName: "stop_container", want: acpsdk.ToolKindDelete},
		{name: "search_ prefix", toolName: "search_files_content", want: acpsdk.ToolKindSearch},
		{name: "find_ prefix", toolName: "find_symbol", want: acpsdk.ToolKindSearch},
		{name: "think", toolName: "think", want: acpsdk.ToolKindThink},
		{name: "fetch", toolName: "fetch", want: acpsdk.ToolKindFetch},
		{name: "http_ prefix", toolName: "http_get", want: acpsdk.ToolKindFetch},
		{name: "shell", toolName: "shell", want: acpsdk.ToolKindExecute},
		{name: "run_ prefix", toolName: "run_tests", want: acpsdk.ToolKindExecute},
		{name: "exec_ prefix", toolName: "exec_command", want: acpsdk.ToolKindExecute},
		{name: "transfer_task", toolName: "transfer_task", want: acpsdk.ToolKindSwitchMode},
		{name: "handoff", toolName: "handoff", want: acpsdk.ToolKindSwitchMode},
		{name: "unknown", toolName: "banana", want: acpsdk.ToolKindOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := determineToolKind(tt.toolName, tools.Tool{Annotations: tt.annotations})
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseToolCallArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args string
		want map[string]any
	}{
		{name: "valid object", args: `{"path":"/tmp","line":42}`, want: map[string]any{"path": "/tmp", "line": float64(42)}},
		{name: "empty object", args: `{}`, want: map[string]any{}},
		{name: "null", args: `null`, want: nil},
		{name: "invalid json wrapped as raw", args: `not-json`, want: map[string]any{"raw": "not-json"}},
		{name: "empty string wrapped as raw", args: ``, want: map[string]any{"raw": ""}},
		{name: "array wrapped as raw", args: `[1,2]`, want: map[string]any{"raw": "[1,2]"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseToolCallArguments(tt.args))
		})
	}
}

func TestExtractLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want []acpsdk.ToolCallLocation
	}{
		{name: "no args", args: map[string]any{}, want: nil},
		{name: "path key", args: map[string]any{"path": "/a.txt"}, want: []acpsdk.ToolCallLocation{{Path: "/a.txt"}}},
		{name: "file key", args: map[string]any{"file": "/b.txt"}, want: []acpsdk.ToolCallLocation{{Path: "/b.txt"}}},
		{name: "filepath key", args: map[string]any{"filepath": "/c.txt"}, want: []acpsdk.ToolCallLocation{{Path: "/c.txt"}}},
		{name: "filename key", args: map[string]any{"filename": "/d.txt"}, want: []acpsdk.ToolCallLocation{{Path: "/d.txt"}}},
		{name: "file_path key", args: map[string]any{"file_path": "/e.txt"}, want: []acpsdk.ToolCallLocation{{Path: "/e.txt"}}},
		{
			name: "first matching path key wins",
			args: map[string]any{"path": "/a.txt", "file": "/b.txt"},
			want: []acpsdk.ToolCallLocation{{Path: "/a.txt"}},
		},
		{
			name: "path with line",
			args: map[string]any{"path": "/a.txt", "line": float64(42)},
			want: []acpsdk.ToolCallLocation{{Path: "/a.txt", Line: new(42)}},
		},
		{name: "empty path ignored", args: map[string]any{"path": ""}, want: nil},
		{name: "non-string path ignored", args: map[string]any{"path": 12}, want: nil},
		{
			name: "paths array",
			args: map[string]any{"paths": []any{"/a.txt", "/b.txt"}},
			want: []acpsdk.ToolCallLocation{{Path: "/a.txt"}, {Path: "/b.txt"}},
		},
		{
			name: "paths array skips empty and non-string entries",
			args: map[string]any{"paths": []any{"", 1, "/ok.txt"}},
			want: []acpsdk.ToolCallLocation{{Path: "/ok.txt"}},
		},
		{
			name: "path and paths combined",
			args: map[string]any{"path": "/a.txt", "paths": []any{"/b.txt"}},
			want: []acpsdk.ToolCallLocation{{Path: "/a.txt"}, {Path: "/b.txt"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractLocations(tt.args))
		})
	}
}

func TestExtractDiffContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		arguments string
		want      *acpsdk.ToolCallContentDiff
	}{
		{
			name:      "edit_file single edit",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":[{"oldText":"a","newText":"b"}]}`,
			want:      &acpsdk.ToolCallContentDiff{Path: "/f.go", OldText: new("a\n"), NewText: "b\n", Type: "diff"},
		},
		{
			name:      "edit_file concatenates edits",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":[{"oldText":"a","newText":"b"},{"oldText":"c","newText":"d"}]}`,
			want:      &acpsdk.ToolCallContentDiff{Path: "/f.go", OldText: new("a\nc\n"), NewText: "b\nd\n", Type: "diff"},
		},
		{
			name:      "edit_file skips non-object edits",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":["bogus",{"oldText":"a","newText":"b"}]}`,
			want:      &acpsdk.ToolCallContentDiff{Path: "/f.go", OldText: new("a\n"), NewText: "b\n", Type: "diff"},
		},
		{
			name:      "edit_file all edits invalid",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":["bogus"]}`,
			want:      nil,
		},
		{
			name:      "edit_file no edits",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":[]}`,
			want:      nil,
		},
		{
			name:      "edit_file edits wrong outer type",
			toolName:  "edit_file",
			arguments: `{"path":"/f.go","edits":{"oldText":"a","newText":"b"}}`,
			want:      nil,
		},
		{
			name:      "edit_file missing path",
			toolName:  "edit_file",
			arguments: `{"edits":[{"oldText":"a","newText":"b"}]}`,
			want:      nil,
		},
		{
			name:      "write_file with content",
			toolName:  "write_file",
			arguments: `{"path":"/f.go","content":"hello"}`,
			want:      &acpsdk.ToolCallContentDiff{Path: "/f.go", NewText: "hello", Type: "diff"},
		},
		{
			name:      "write_file without content",
			toolName:  "write_file",
			arguments: `{"path":"/f.go"}`,
			want:      nil,
		},
		{
			name:      "write_file content wrong outer type",
			toolName:  "write_file",
			arguments: `{"path":"/f.go","content":["hello"]}`,
			want:      nil,
		},
		{
			name:      "unrelated tool",
			toolName:  "read_file",
			arguments: `{"path":"/f.go","content":"hello"}`,
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractDiffContent(tt.toolName, tt.arguments)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.Diff)
		})
	}
}

func TestBuildToolCallComplete(t *testing.T) {
	t.Parallel()

	editArgs := `{"path":"/f.go","edits":[{"oldText":"a","newText":"b"}]}`

	tests := []struct {
		name       string
		arguments  string
		event      *runtime.ToolCallResponseEvent
		wantStatus acpsdk.ToolCallStatus
		wantDiff   bool
	}{
		{
			name:      "nil result completes with text content",
			arguments: `{"command":"ls"}`,
			event: &runtime.ToolCallResponseEvent{
				ToolCallID:     "call-1",
				ToolDefinition: tools.Tool{Name: "shell"},
				Response:       "file.txt",
			},
			wantStatus: acpsdk.ToolCallStatusCompleted,
		},
		{
			name:      "error result fails",
			arguments: `{"command":"ls"}`,
			event: &runtime.ToolCallResponseEvent{
				ToolCallID:     "call-1",
				ToolDefinition: tools.Tool{Name: "shell"},
				Response:       "denied",
				Result:         tools.ResultError("denied"),
			},
			wantStatus: acpsdk.ToolCallStatusFailed,
		},
		{
			name:      "successful edit_file uses diff content",
			arguments: editArgs,
			event: &runtime.ToolCallResponseEvent{
				ToolCallID:     "call-1",
				ToolDefinition: tools.Tool{Name: "edit_file"},
				Response:       "edited",
				Result:         tools.ResultSuccess("edited"),
			},
			wantStatus: acpsdk.ToolCallStatusCompleted,
			wantDiff:   true,
		},
		{
			name:      "failed edit_file keeps text content",
			arguments: editArgs,
			event: &runtime.ToolCallResponseEvent{
				ToolCallID:     "call-1",
				ToolDefinition: tools.Tool{Name: "edit_file"},
				Response:       "failed",
				Result:         tools.ResultError("failed"),
			},
			wantStatus: acpsdk.ToolCallStatusFailed,
		},
		{
			name:      "edit_file without diffable args falls back to text",
			arguments: `{"path":"/f.go","edits":[]}`,
			event: &runtime.ToolCallResponseEvent{
				ToolCallID:     "call-1",
				ToolDefinition: tools.Tool{Name: "edit_file"},
				Response:       "edited",
				Result:         tools.ResultSuccess("edited"),
			},
			wantStatus: acpsdk.ToolCallStatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			update := buildToolCallComplete(tt.arguments, tt.event)
			require.NotNil(t, update.ToolCallUpdate)
			tc := update.ToolCallUpdate

			assert.Equal(t, acpsdk.ToolCallId(tt.event.ToolCallID), tc.ToolCallId)
			require.NotNil(t, tc.Status)
			assert.Equal(t, tt.wantStatus, *tc.Status)
			assert.Equal(t, map[string]any{"content": tt.event.Response}, tc.RawOutput)

			require.Len(t, tc.Content, 1)
			if tt.wantDiff {
				require.NotNil(t, tc.Content[0].Diff)
				assert.Equal(t, "/f.go", tc.Content[0].Diff.Path)
				assert.Equal(t, "b\n", tc.Content[0].Diff.NewText)
				require.NotNil(t, tc.Content[0].Diff.OldText)
				assert.Equal(t, "a\n", *tc.Content[0].Diff.OldText)
			} else {
				require.NotNil(t, tc.Content[0].Content)
				require.NotNil(t, tc.Content[0].Content.Content.Text)
				assert.Equal(t, tt.event.Response, tc.Content[0].Content.Content.Text.Text)
			}
		})
	}
}

func TestBuildToolCallStart(t *testing.T) {
	t.Parallel()

	t.Run("uses annotation title and extracts locations", func(t *testing.T) {
		t.Parallel()

		toolCall := tools.ToolCall{
			ID:       "call-1",
			Function: tools.FunctionCall{Name: "read_file", Arguments: `{"path":"/a.txt"}`},
		}
		tool := tools.Tool{Name: "read_file", Annotations: tools.ToolAnnotations{Title: "Read File", ReadOnlyHint: true}}

		update := buildToolCallStart(toolCall, tool)
		require.NotNil(t, update.ToolCall)
		tc := update.ToolCall

		assert.Equal(t, acpsdk.ToolCallId("call-1"), tc.ToolCallId)
		assert.Equal(t, "Read File", tc.Title)
		assert.Equal(t, acpsdk.ToolKindRead, tc.Kind)
		assert.Equal(t, acpsdk.ToolCallStatusPending, tc.Status)
		assert.Equal(t, map[string]any{"path": "/a.txt"}, tc.RawInput)
		assert.Equal(t, []acpsdk.ToolCallLocation{{Path: "/a.txt"}}, tc.Locations)
	})

	t.Run("falls back to function name without locations", func(t *testing.T) {
		t.Parallel()

		toolCall := tools.ToolCall{
			ID:       "call-2",
			Function: tools.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`},
		}

		update := buildToolCallStart(toolCall, tools.Tool{Name: "shell"})
		require.NotNil(t, update.ToolCall)
		tc := update.ToolCall

		assert.Equal(t, "shell", tc.Title)
		assert.Equal(t, acpsdk.ToolKindExecute, tc.Kind)
		assert.Empty(t, tc.Locations)
	})
}

func TestBuildToolCallUpdate(t *testing.T) {
	t.Parallel()

	toolCall := tools.ToolCall{
		ID:       "call-1",
		Function: tools.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`},
	}

	update := buildToolCallUpdate(toolCall, tools.Tool{Name: "shell"}, acpsdk.ToolCallStatusPending)
	assert.Equal(t, acpsdk.ToolCallId("call-1"), update.ToolCallId)
	require.NotNil(t, update.Title)
	assert.Equal(t, "shell", *update.Title)
	require.NotNil(t, update.Kind)
	assert.Equal(t, acpsdk.ToolKindExecute, *update.Kind)
	require.NotNil(t, update.Status)
	assert.Equal(t, acpsdk.ToolCallStatusPending, *update.Status)
	assert.Equal(t, map[string]any{"command": "ls"}, update.RawInput)

	readOnly := tools.Tool{Name: "shell", Annotations: tools.ToolAnnotations{ReadOnlyHint: true}}
	update = buildToolCallUpdate(toolCall, readOnly, acpsdk.ToolCallStatusCompleted)
	require.NotNil(t, update.Kind)
	assert.Equal(t, acpsdk.ToolKindRead, *update.Kind)
}

func TestIsTodoTool(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		todo.ToolNameCreateTodo,
		todo.ToolNameCreateTodos,
		todo.ToolNameUpdateTodos,
		todo.ToolNameListTodos,
	} {
		assert.True(t, isTodoTool(name), name)
	}
	assert.False(t, isTodoTool("todo"))
	assert.False(t, isTodoTool("shell"))
}

func TestBuildPlanUpdateFromTodos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta any
		want []acpsdk.PlanEntry
	}{
		{name: "not a todo slice", meta: "bogus", want: nil},
		{name: "nil meta", meta: nil, want: nil},
		{name: "empty todos", meta: []todo.Todo{}, want: nil},
		{
			name: "todos map to plan entries",
			meta: []todo.Todo{
				{ID: "1", Description: "write tests", Status: "pending"},
				{ID: "2", Description: "run tests", Status: "in-progress"},
				{ID: "3", Description: "review", Status: "completed"},
			},
			want: []acpsdk.PlanEntry{
				{Content: "write tests", Status: acpsdk.PlanEntryStatusPending, Priority: acpsdk.PlanEntryPriorityMedium},
				{Content: "run tests", Status: acpsdk.PlanEntryStatusInProgress, Priority: acpsdk.PlanEntryPriorityMedium},
				{Content: "review", Status: acpsdk.PlanEntryStatusCompleted, Priority: acpsdk.PlanEntryPriorityMedium},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			update := buildPlanUpdateFromTodos(tt.meta)
			if tt.want == nil {
				assert.Nil(t, update)
				return
			}
			require.NotNil(t, update)
			require.NotNil(t, update.Plan)
			assert.Equal(t, tt.want, update.Plan.Entries)
		})
	}
}

func TestMapTodoStatusToACP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   acpsdk.PlanEntryStatus
	}{
		{status: "pending", want: acpsdk.PlanEntryStatusPending},
		{status: "in-progress", want: acpsdk.PlanEntryStatusInProgress},
		{status: "completed", want: acpsdk.PlanEntryStatusCompleted},
		{status: "", want: acpsdk.PlanEntryStatusPending},
		{status: "unknown", want: acpsdk.PlanEntryStatusPending},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, mapTodoStatusToACP(tt.status), "status %q", tt.status)
	}
}
