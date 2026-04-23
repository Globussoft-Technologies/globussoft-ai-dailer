// Package llm provides the gRPC bridge to Python's LLM logic and a sentence splitter.
package llm

import (
	"regexp"
	"strings"
)

// sentenceRE matches Python's re.compile(r'([.!?|\n]+)').
var sentenceRE = regexp.MustCompile(`[.!?|\n]+`)

// SplitBuffer splits accumulated LLM output into complete sentences and a remainder.
// Mirrors the Python ws_handler.py sentence-splitting logic:
//
//	parts = re.split(r'([.!?|\n]+)', buffer)
//	# pairs of (text, delimiter) → sentences; last element is remainder
//
// Returns (sentences, remainder). Each sentence already has its delimiter attached.
func SplitBuffer(buf string) (sentences []string, remainder string) {
	locs := sentenceRE.FindAllStringIndex(buf, -1)
	if len(locs) == 0 {
		return nil, buf
	}
	prev := 0
	for _, loc := range locs {
		text := buf[prev:loc[0]]
		delim := buf[loc[0]:loc[1]]
		sentence := strings.TrimSpace(text + delim)
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
		prev = loc[1]
	}
	remainder = buf[prev:]
	return
}
