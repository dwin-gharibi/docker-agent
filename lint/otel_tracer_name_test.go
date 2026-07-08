package main

import (
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOTelTracerNameFlagsCagent(t *testing.T) {
	t.Parallel()
	src := `package p
import "go.opentelemetry.io/otel"
func f() { _ = otel.Tracer("cagent") }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Equal(t, "Lint/OTelTracerName", offenses[0].CopName)
	assert.Contains(t, offenses[0].Message, `otel.Tracer(AppName)`)
}

func TestOTelTracerNameFlagsLocalAppName(t *testing.T) {
	t.Parallel()
	// A local const named AppName from an unrelated package must not bypass the cop.
	src := `package p
import "go.opentelemetry.io/otel"
const AppName = "docker-agent"
func f() { _ = otel.Tracer(AppName) }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, `got "docker-agent"`)
}

func TestOTelTracerNameAllowsVersionAppName(t *testing.T) {
	t.Parallel()
	src := `package p
import (
	"go.opentelemetry.io/otel"
	"github.com/docker/docker-agent/pkg/version"
)
func f() { _ = otel.Tracer(version.AppName) }
`
	assert.Empty(t, coptest.RunTyped(t, OTelTracerName, src))
}

func TestOTelTracerNameFlagsWrongName(t *testing.T) {
	t.Parallel()
	src := `package p
import "go.opentelemetry.io/otel"
func f() { _ = otel.Tracer("wrong") }
`
	offenses := coptest.RunTyped(t, OTelTracerName, src)
	require.Len(t, offenses, 1)
	assert.Contains(t, offenses[0].Message, `got "wrong"`)
}
