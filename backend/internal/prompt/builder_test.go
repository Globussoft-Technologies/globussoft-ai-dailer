package prompt

import (
	"strings"
	"testing"

	"github.com/globussoft/callified-backend/internal/db"
	"github.com/stretchr/testify/assert"
)

func TestRenderCallMemoryEmpty(t *testing.T) {
	assert.Equal(t, "", renderCallMemory(nil))
	assert.Equal(t, "", renderCallMemory([]db.CallMemory{}))
}

func TestRenderCallMemoryIncludesFields(t *testing.T) {
	out := renderCallMemory([]db.CallMemory{
		{
			CreatedAt:     "2026-09-01",
			Summary:       "Customer asked about EMI options and said price is too high.",
			FailureReason: "Pricing objection not handled",
			Suggestion:    "Offer the festival discount before quoting EMI.",
		},
	})

	assert.Contains(t, out, "## PREVIOUS CALLS WITH THIS CUSTOMER")
	assert.Contains(t, out, "2026-09-01")
	assert.Contains(t, out, "EMI options")
	assert.Contains(t, out, "Pricing objection not handled")
	assert.Contains(t, out, "festival discount")
	// Guardrail instruction must be present so the agent never speaks the notes.
	assert.Contains(t, out, "never speak")
	// Off-topic details from dirty notes must be ignored.
	assert.Contains(t, out, "unrelated to the product")
}

// The memory block must precede the CALL FLOW questionnaire: confirmed
// facts established before the script beats an afterthought at the end.
// With memory + a product questionnaire, the questionnaire must be replaced
// by the confirm-and-book flow — not rendered alongside it.
func TestDefaultPromptPlacesMemoryBeforeCallFlow(t *testing.T) {
	out := buildDefaultPrompt(promptContext{
		Language:             "en",
		CallMemory:           renderCallMemory([]db.CallMemory{{CreatedAt: "2026-09-03", Summary: "15 users, Bengaluru"}}),
		CompanyName:          "ACME",
		CallFlowInstructions: "QUESTIONNAIRE: ask how many users, which sector, how many locations.",
	})

	memIdx := strings.Index(out, "## PREVIOUS CALLS WITH THIS CUSTOMER")
	flowIdx := strings.Index(out, "## CALL FLOW")
	assert.True(t, memIdx > 0, "memory block missing")
	assert.True(t, flowIdx > memIdx, "memory block must come before CALL FLOW")
	assert.Contains(t, out, "CONTINUATION CALL")
	assert.NotContains(t, out, "QUESTIONNAIRE", "questionnaire must be replaced when memory exists")
}

// Without memory the product questionnaire stays untouched.
func TestDefaultPromptWithoutMemoryKeepsQuestionnaire(t *testing.T) {
	out := buildDefaultPrompt(promptContext{
		Language:             "en",
		CompanyName:          "ACME",
		CallFlowInstructions: "QUESTIONNAIRE: ask how many users, which sector, how many locations.",
	})

	assert.NotContains(t, out, "## PREVIOUS CALLS WITH THIS CUSTOMER")
	assert.NotContains(t, out, "CONTINUATION CALL")
	assert.Contains(t, out, "QUESTIONNAIRE")
}

func TestRenderCallMemoryOmitsEmptyFields(t *testing.T) {
	out := renderCallMemory([]db.CallMemory{
		{CreatedAt: "2026-09-02", Summary: "No answer details"},
	})

	assert.Contains(t, out, "What happened: No answer details")
	assert.NotContains(t, out, "What went wrong:")
	assert.NotContains(t, out, "Do better this time:")
}

func TestRenderCallMemoryCapsFieldLength(t *testing.T) {
	long := strings.Repeat("x", 500)
	out := renderCallMemory([]db.CallMemory{
		{CreatedAt: "2026-09-03", Summary: long},
	})

	// Field is truncated to callMemoryMaxFieldLen + ellipsis.
	assert.Contains(t, out, strings.Repeat("x", callMemoryMaxFieldLen)+"…")
	assert.NotContains(t, out, strings.Repeat("x", callMemoryMaxFieldLen+1))
}

func TestClampRunes(t *testing.T) {
	assert.Equal(t, "hello", clampRunes("hello", 10))
	assert.Equal(t, "hello", clampRunes("  hello  ", 10))
	assert.Equal(t, "hell…", clampRunes("hello world", 4))
	assert.Equal(t, "", clampRunes("   ", 4))
}
