package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafetyPolicy_IsValid(t *testing.T) {
	t.Parallel()
	cases := map[SafetyPolicy]bool{
		"":                    true,
		SafetyPolicyUnsafe:    true,
		SafetyPolicySafer:     true,
		SafetyPolicySafeAuto:  true,
		SafetyPolicyStrict:    true,
		SafetyPolicy("yolo"):  false,
		SafetyPolicy("Safer"): false, // case-sensitive on purpose
	}
	for in, want := range cases {
		assert.Equalf(t, want, in.IsValid(), "SafetyPolicy(%q).IsValid()", string(in))
	}
}

// WithSafetyPolicy(unsafe) must flip ToolsApproved=true so legacy
// branches on ToolsApproved (Decide's --yolo short-circuit) still fire.
// Safer/strict intentionally leave ToolsApproved alone.
func TestWithSafetyPolicy_UnsafeSyncsToolsApproved(t *testing.T) {
	t.Parallel()
	s := New(WithSafetyPolicy(SafetyPolicyUnsafe))
	assert.Equal(t, SafetyPolicyUnsafe, s.SafetyPolicy)
	assert.True(t, s.ToolsApproved)

	s = New(WithSafetyPolicy(SafetyPolicySafer))
	assert.Equal(t, SafetyPolicySafer, s.SafetyPolicy)
	assert.False(t, s.ToolsApproved)
}

// WithToolsApproved(true) must backfill SafetyPolicy=unsafe so hooks
// reading Input.SafetyPolicy see the correct value for legacy --yolo
// callers (gordon-Slack, MCP, eval) that haven't migrated.
func TestWithToolsApproved_BackfillsSafetyPolicy(t *testing.T) {
	t.Parallel()
	s := New(WithToolsApproved(true))
	assert.True(t, s.ToolsApproved)
	assert.Equal(t, SafetyPolicyUnsafe, s.SafetyPolicy)

	s = New(WithToolsApproved(false))
	assert.False(t, s.ToolsApproved)
	assert.Equal(t, SafetyPolicy(""), s.SafetyPolicy)
}

// Explicit WithSafetyPolicy after WithToolsApproved wins over the
// backfill (e.g. yolo + safer = "auto-approve except destructive").
func TestWithSafetyPolicy_ExplicitWinsOverToolsApproved(t *testing.T) {
	t.Parallel()
	s := New(
		WithToolsApproved(true),
		WithSafetyPolicy(SafetyPolicySafer),
	)
	assert.True(t, s.ToolsApproved)
	assert.Equal(t, SafetyPolicySafer, s.SafetyPolicy)
}

// SetSafetyPolicy mid-session mirrors WithSafetyPolicy: setting
// unsafe backfills ToolsApproved; other modes leave it alone.
// Used by the dispatcher's approve-safe resume handler to opt an
// existing session into safe-auto without recreating it.
func TestSetSafetyPolicy_MidSession(t *testing.T) {
	t.Parallel()
	s := New()
	assert.Equal(t, SafetyPolicy(""), s.SafetyPolicy)
	assert.False(t, s.ToolsApproved)

	s.SetSafetyPolicy(SafetyPolicySafeAuto)
	assert.Equal(t, SafetyPolicySafeAuto, s.SafetyPolicy)
	assert.False(t, s.ToolsApproved, "safe-auto must not backfill ToolsApproved")

	s.SetSafetyPolicy(SafetyPolicyUnsafe)
	assert.Equal(t, SafetyPolicyUnsafe, s.SafetyPolicy)
	assert.True(t, s.ToolsApproved, "unsafe must backfill ToolsApproved for legacy branches")

	s.SetSafetyPolicy(SafetyPolicyStrict)
	assert.Equal(t, SafetyPolicyStrict, s.SafetyPolicy)
	assert.True(t, s.ToolsApproved, "SetSafetyPolicy does not un-flip ToolsApproved")
}
