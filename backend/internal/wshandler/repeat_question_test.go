package wshandler

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestRepeatedQuestionInstructionEscalatesThenCloses(t *testing.T) {
	sess := NewCallSession("web_sim_repeat", nil, zap.NewNop())

	first := sess.RepeatedQuestionDecision("What company is this?")
	assert.Empty(t, first.Instruction)
	assert.True(t, first.AllowHangup)

	second := sess.RepeatedQuestionDecision("what company is this")
	assert.Contains(t, second.Instruction, "2nd total time")
	assert.False(t, second.AllowHangup)

	third := sess.RepeatedQuestionDecision("What company is this?")
	assert.Contains(t, third.Instruction, "3 total time")
	assert.Contains(t, third.Instruction, "senior teammate should call back")
	assert.Contains(t, third.Instruction, "Do not include [HANGUP]")
	assert.Contains(t, third.Instruction, "do not say you already answered")
	assert.False(t, third.AllowHangup)
	assert.False(t, third.FinalClose)

	fourth := sess.RepeatedQuestionDecision("What company is this?")
	assert.Contains(t, fourth.Instruction, "4 total time")
	assert.Contains(t, fourth.Instruction, "senior teammate will follow up")
	assert.Contains(t, fourth.Instruction, "do not say you already answered")
	assert.Contains(t, fourth.Instruction, "end with [HANGUP]")
	assert.True(t, fourth.AllowHangup)
	assert.True(t, fourth.FinalClose)
}

func TestRepeatedQuestionInstructionIgnoresDifferentQuestions(t *testing.T) {
	sess := NewCallSession("web_sim_repeat", nil, zap.NewNop())

	assert.Empty(t, sess.RepeatedQuestionDecision("What company is this?").Instruction)
	assert.Empty(t, sess.RepeatedQuestionDecision("What product do you sell?").Instruction)
}

func TestNormalizeQuestionTextKeepsUnicodeLetters(t *testing.T) {
	normalized := normalizeQuestionText("  హలో, ఎవరు మాట్లాడుతున్నారు?  ")

	assert.True(t, strings.Contains(normalized, "హలో"))
	assert.NotContains(t, normalized, "?")
}

func TestRepeatedQuestionInstructionGroupsTeluguBiometricsMeaning(t *testing.T) {
	sess := NewCallSession("web_sim_repeat_te", nil, zap.NewNop())
	intentKey := "ask_biometrics_meaning"

	first := sess.RepeatedQuestionDecisionWithKey("Cheppandi What is biometrics?", intentKey)
	assert.Empty(t, first.Instruction)

	second := sess.RepeatedQuestionDecisionWithKey("అవును, మీరు ఇంకోసారి చెప్పండి బయోమెట్రిక్స్ అంటే ఏంటి నాకు అర్థం అవ్వలేదు.", intentKey)
	assert.Contains(t, second.Instruction, "2nd total time")

	third := sess.RepeatedQuestionDecisionWithKey("కొత్త ఆఫీస్ సెటప్ కావాలి. బయోమెట్రిక్స్ అంటే ఏంటో ఇంకోసారి చెప్పండి.", intentKey)
	assert.Contains(t, third.Instruction, "3 total time")
	assert.False(t, third.AllowHangup)

	fourth := sess.RepeatedQuestionDecisionWithKey("చెప్పండి, బయోమెట్రిక్స్ అంటే ఏంటి?", intentKey)
	assert.Contains(t, fourth.Instruction, "4 total time")
	assert.True(t, fourth.AllowHangup)
	assert.True(t, fourth.FinalClose)
}

func TestRepeatedQuestionInstructionUsesSemanticIntentAcrossLanguages(t *testing.T) {
	sess := NewCallSession("web_sim_repeat_multi_lang", nil, zap.NewNop())

	assert.Empty(t, sess.RepeatedQuestionDecisionWithKey("What is the price?", "ask_price").Instruction)
	assert.Contains(t, sess.RepeatedQuestionDecisionWithKey("ధర ఎంత?", "ask_price").Instruction, "2nd total time")
	assert.Contains(t, sess.RepeatedQuestionDecisionWithKey("Hindi mein price batao", "ask_price").Instruction, "3 total time")
	assert.Contains(t, sess.RepeatedQuestionDecisionWithKey("Tell me the cost again", "ask_price").Instruction, "4 total time")
}
