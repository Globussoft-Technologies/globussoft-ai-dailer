package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// SplitBuffer keeps the delimiter attached to the sentence, matching Python's
// re.split(r'([.!?|\n]+)', buffer) behavior where pairs are (text, delimiter).

func TestSplitBuffer_SingleSentence(t *testing.T) {
	sentences, rem := SplitBuffer("Hello, how are you?")
	assert.Equal(t, []string{"Hello, how are you?"}, sentences)
	assert.Equal(t, "", rem)
}

func TestSplitBuffer_MultipleSentences(t *testing.T) {
	sentences, rem := SplitBuffer("Hello. How are you? I am fine!")
	assert.Equal(t, []string{"Hello.", "How are you?", "I am fine!"}, sentences)
	assert.Equal(t, "", rem)
}

func TestSplitBuffer_Remainder(t *testing.T) {
	sentences, rem := SplitBuffer("Hello. How are you")
	assert.Equal(t, []string{"Hello."}, sentences)
	// Remainder starts at position after delimiter — leading space is preserved
	assert.Equal(t, " How are you", rem)
}

func TestSplitBuffer_EmptyInput(t *testing.T) {
	sentences, rem := SplitBuffer("")
	assert.Empty(t, sentences)
	assert.Equal(t, "", rem)
}

func TestSplitBuffer_NoDelimiter(t *testing.T) {
	sentences, rem := SplitBuffer("Hello world")
	assert.Empty(t, sentences)
	assert.Equal(t, "Hello world", rem)
}

func TestSplitBuffer_HindiPipe(t *testing.T) {
	// Hindi sentences often use | as sentence terminator; delimiter is kept
	sentences, rem := SplitBuffer("Namaste| Kaise hain aap|")
	assert.Equal(t, []string{"Namaste|", "Kaise hain aap|"}, sentences)
	assert.Equal(t, "", rem)
}

func TestSplitBuffer_NewlineSplit(t *testing.T) {
	// TrimSpace strips the trailing newline delimiter from each sentence
	sentences, rem := SplitBuffer("Line one\nLine two\n")
	assert.Equal(t, []string{"Line one", "Line two"}, sentences)
	assert.Equal(t, "", rem)
}

func TestSplitBuffer_MultipleDelimiters(t *testing.T) {
	// "..." should be consumed as a single delimiter token
	sentences, rem := SplitBuffer("Hmm... Okay. Got it")
	assert.Equal(t, []string{"Hmm...", "Okay."}, sentences)
	assert.Equal(t, " Got it", rem)
}

func TestSplitBuffer_WhitespaceTrimmedFromSentences(t *testing.T) {
	// Leading/trailing whitespace inside each sentence is trimmed by TrimSpace
	sentences, _ := SplitBuffer("  Hello.  World?  ")
	assert.Equal(t, []string{"Hello.", "World?"}, sentences)
}
