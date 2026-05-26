package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the shape-repair behavior NewHandler relies on. The
// repair logic itself lives in github.com/docker/aijson; what's tested here
// is the integration: that arguments NewHandler observes in practice are
// repaired before the typed handler runs, and that unrepairable input
// surfaces the original error rather than a synthesised one.

// argsWithStrings exercises the slice-of-string repair path that's by far
// the most commonly broken in real LLM tool calls (paths, urls, patterns).
type argsWithStrings struct {
	Paths []string `json:"paths"`
	JSON  bool     `json:"json,omitempty"`
}

type argsWithInt struct {
	N    int      `json:"n"`
	Tags []string `json:"tags,omitempty"`
}

func runHandler[T any](t *testing.T, args string) (T, error) {
	t.Helper()
	var got T
	handler := NewHandler(func(_ context.Context, a T) (*ToolCallResult, error) {
		got = a
		return ResultSuccess("ok"), nil
	})
	_, err := handler(t.Context(), ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: FunctionCall{
			Name:      "test_tool",
			Arguments: args,
		},
	})
	return got, err
}

func TestNewHandler_UnwrapsStringifiedArray(t *testing.T) {
	// Common DeepSeek/Qwen mistake: send an array as a JSON string.
	got, err := runHandler[argsWithStrings](t, `{"paths":"[\"a.txt\",\"b.txt\"]"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.txt", "b.txt"}, got.Paths)
}

func TestNewHandler_WrapsBareString(t *testing.T) {
	// Single-string-instead-of-array, the most common shape mistake.
	got, err := runHandler[argsWithStrings](t, `{"paths":"only.txt"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"only.txt"}, got.Paths)
}

func TestNewHandler_WrapsSingleObjectPlaceholder(t *testing.T) {
	// Some models wrap a single argument in an object.
	got, err := runHandler[argsWithStrings](t, `{"paths":{"path":"only.txt"}}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"only.txt"}, got.Paths)
}

func TestNewHandler_OrderingPreventsDoubleWrap(t *testing.T) {
	// If the bare-string-wrap fired before the unwrap-stringified-array
	// repair, this input would become [`["a","b"]`] instead of ["a","b"].
	// The clean two-element slice is the load-bearing assertion.
	got, err := runHandler[argsWithStrings](t, `{"paths":"[\"a\",\"b\"]"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got.Paths)
}

func TestNewHandler_DropsNullForPrimitive(t *testing.T) {
	// Some custom UnmarshalJSON impls trip on null where a primitive is
	// expected. Dropping the field lets the type's zero value win.
	got, err := runHandler[argsWithInt](t, `{"n":null}`)
	require.NoError(t, err)
	assert.Equal(t, 0, got.N)
}

func TestNewHandler_LeavesValidArrayUntouched(t *testing.T) {
	got, err := runHandler[argsWithStrings](t, `{"paths":["a","b"]}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got.Paths)
}

// TestNewHandler_VisitsPromotedFieldsFromEmbeddedStruct mirrors the shape
// of LSP arg structs in pkg/tools/builtin/lsp (ReferencesArgs and friends
// embed PositionArgs). Repairs must reach promoted fields, otherwise LSP
// tools regress silently when models send a single file instead of an
// array.
func TestNewHandler_VisitsPromotedFieldsFromEmbeddedStruct(t *testing.T) {
	type Base struct {
		Files []string `json:"files"`
	}
	type WithEmbedding struct {
		Base

		Extra string `json:"extra,omitempty"`
	}
	got, err := runHandler[WithEmbedding](t, `{"files":"only.txt"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"only.txt"}, got.Files)
}

func TestNewHandler_RepairsMultipleFieldsInOneCall(t *testing.T) {
	type combo struct {
		Paths []string `json:"paths"`
		Tags  []string `json:"tags"`
	}
	got, err := runHandler[combo](t, `{"paths":"only.txt","tags":"[\"go\",\"ai\"]"}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"only.txt"}, got.Paths)
	assert.Equal(t, []string{"go", "ai"}, got.Tags)
}

func TestNewHandler_RefusesMultiKeyObjectAsArray(t *testing.T) {
	// Two keys in the placeholder object — too ambiguous to safely wrap.
	// Surface the original schema error rather than guessing.
	_, err := runHandler[argsWithStrings](t, `{"paths":{"path":"a.txt","extra":"ignore"}}`)
	require.Error(t, err)
}

func TestNewHandler_UnrepairableInputReturnsOriginalError(t *testing.T) {
	type fileArgs struct {
		Paths []string `json:"paths"`
	}
	handler := NewHandler(func(_ context.Context, _ fileArgs) (*ToolCallResult, error) {
		t.Fatal("handler should not be called for unrepairable input")
		return nil, nil
	})

	_, err := handler(t.Context(), ToolCall{
		ID:   "call_1",
		Type: "function",
		Function: FunctionCall{
			Name:      "read_multiple_files",
			Arguments: `{not even json`,
		},
	})
	require.Error(t, err)
}
