package root

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	loadertoolsets "github.com/docker/docker-agent/pkg/teamloader/toolsets"
)

func TestToolsetsCommand_TableListsBuiltinTypes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newToolsetsCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)

	require.NoError(t, cmd.Execute())

	out := buf.String()
	require.Contains(t, out, "TYPE")
	require.Contains(t, out, "SUMMARY")
	require.Contains(t, out, "think")
	require.Contains(t, out, "filesystem")
	require.Contains(t, out, "Step-by-step reasoning scratchpad for planning")
}

func TestToolsetsCommand_JSONMatchesCatalog(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newToolsetsCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--format", "json"})

	require.NoError(t, cmd.Execute())

	var got []loadertoolsets.BuiltinToolsetInfo
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Equal(t, loadertoolsets.BuiltinToolsets, got)
}

func TestToolsetsCommand_RejectsArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newToolsetsCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unexpected"})

	require.Error(t, cmd.Execute())
}

func TestToolsetsCommand_RejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newToolsetsCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--format", "yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "format")
}
