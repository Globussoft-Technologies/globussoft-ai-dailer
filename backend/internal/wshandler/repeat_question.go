package wshandler

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	repeatQuestionWindow       = 4 * time.Minute
	repeatQuestionMaxTracked   = 6
	repeatQuestionSimilarity   = 0.78
	repeatQuestionEscalateAt   = 3
	repeatQuestionCloseAt      = 4
	repeatQuestionMinRuneCount = 4
)

type repeatQuestion struct {
	Normalized string
	Count      int
	SeenAt     time.Time
}

type repeatQuestionDecision struct {
	Instruction string
	AllowHangup bool
	FinalClose  bool
}

func (s *CallSession) RepeatedQuestionDecision(text string) repeatQuestionDecision {
	return s.RepeatedQuestionDecisionWithKey(text, "")
}

func (s *CallSession) RepeatedQuestionDecisionWithKey(text, intentKey string) repeatQuestionDecision {
	norm := normalizeQuestionText(text)
	if key := normalizeQuestionText(intentKey); key != "" && key != "none" {
		norm = "intent " + key
	}
	if len([]rune(norm)) < repeatQuestionMinRuneCount || isFillerSound(norm) {
		return repeatQuestionDecision{AllowHangup: true}
	}

	now := time.Now()
	s.repeatMu.Lock()
	defer s.repeatMu.Unlock()

	pruned := s.recentQuestions[:0]
	for _, q := range s.recentQuestions {
		if now.Sub(q.SeenAt) <= repeatQuestionWindow {
			pruned = append(pruned, q)
		}
	}
	s.recentQuestions = pruned

	best := -1
	bestScore := 0.0
	for i, q := range s.recentQuestions {
		score := questionSimilarity(norm, q.Normalized)
		if score > bestScore {
			bestScore = score
			best = i
		}
	}

	if best == -1 || bestScore < repeatQuestionSimilarity {
		s.recentQuestions = append(s.recentQuestions, repeatQuestion{
			Normalized: norm,
			Count:      1,
			SeenAt:     now,
		})
		s.trimRecentQuestionsLocked()
		return repeatQuestionDecision{AllowHangup: true}
	}

	s.recentQuestions[best].Count++
	s.recentQuestions[best].SeenAt = now
	count := s.recentQuestions[best].Count
	s.trimRecentQuestionsLocked()

	switch {
	case count >= repeatQuestionCloseAt:
		return repeatQuestionDecision{
			Instruction: fmt.Sprintf("[REPEATED CUSTOMER QUESTION: This is the customer's %d total time asking the same question. Do not blame the customer and do not say you already answered. Give the short direct answer one last time, say the line may not be clear, say a senior teammate will follow up and explain better, thank them, and end with [HANGUP].]", count),
			AllowHangup: true,
			FinalClose:  true,
		}
	case count >= repeatQuestionEscalateAt:
		return repeatQuestionDecision{
			Instruction: fmt.Sprintf("[REPEATED CUSTOMER QUESTION: This is the customer's %d total time asking the same question. Do not end the call. Do not include [HANGUP]. Do not blame the customer and do not say you already answered. Give the short direct answer in simpler words, then say the line may not be clear and ask if a senior teammate should call back and explain better.]", count),
			AllowHangup: false,
		}
	case count == 2:
		return repeatQuestionDecision{
			Instruction: "[REPEATED CUSTOMER QUESTION: This is the customer's 2nd total time asking the same question. Give a short direct answer in different words, then continue only after they confirm. Do not end the call and do not include [HANGUP].]",
			AllowHangup: false,
		}
	default:
		return repeatQuestionDecision{AllowHangup: true}
	}
}

func (s *CallSession) trimRecentQuestionsLocked() {
	if len(s.recentQuestions) <= repeatQuestionMaxTracked {
		return
	}
	s.recentQuestions = append([]repeatQuestion(nil), s.recentQuestions[len(s.recentQuestions)-repeatQuestionMaxTracked:]...)
}

func normalizeQuestionText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	var b strings.Builder
	lastSpace := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
			lastSpace = false
		case unicode.IsSpace(r):
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func questionSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ar, br := []rune(a), []rune(b)
	longest := len(ar)
	if len(br) > longest {
		longest = len(br)
	}
	if longest == 0 {
		return 1
	}
	distance := levenshteinRunes(ar, br)
	return 1 - float64(distance)/float64(longest)
}

func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minInt(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt(values ...int) int {
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
