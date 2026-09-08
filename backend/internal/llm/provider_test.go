package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeRepeatIntentKey(t *testing.T) {
	assert.Equal(t, "ask_price", sanitizeRepeatIntentKey("`Ask Price.`"))
	assert.Equal(t, "ask_biometrics_meaning", sanitizeRepeatIntentKey("ask biometrics meaning"))
	assert.Empty(t, sanitizeRepeatIntentKey("none"))
	assert.Empty(t, sanitizeRepeatIntentKey("Greeting."))
}

func TestBracketedMetaFilterStripsBeforeSentenceSplitting(t *testing.T) {
	filter := newBracketedMetaFilter()

	assert.Equal(t, "", filter.Write("[Customer interrupted. "))
	assert.Equal(t, "", filter.Write("The agent needs to re-ask the current question.]"))
	assert.Equal(t, " Okay sri.", filter.Write(" Okay sri."))
}

func TestBracketedMetaFilterPreservesHangupSignal(t *testing.T) {
	filter := newBracketedMetaFilter()

	assert.Equal(t, "Thank you. ", filter.Write("Thank you. "))
	assert.Equal(t, "[HANGUP]", filter.Write("[HANGUP]"))
}

func TestParseChunkStripsUnbracketedControlNarration(t *testing.T) {
	text, hangup := parseChunk(`: The customer's response "How many" is unclear. It does not directly answer my previous question. I need to re-ask it. I will keep it simple. శ్రీ గారు, మీరు అటెండెన్స్ కోసం చూస్తున్నారా?`)

	assert.False(t, hangup)
	assert.Equal(t, "శ్రీ గారు, మీరు అటెండెన్స్ కోసం చూస్తున్నారా?", text)
}

func TestParseChunkStripsAgentNeedsNarration(t *testing.T) {
	text, hangup := parseChunk(`The agent needs to re-ask the current question. ] Okay sri. Are you looking for attendance or access management?`)

	assert.False(t, hangup)
	assert.Equal(t, "Okay sri. Are you looking for attendance or access management?", text)
}
