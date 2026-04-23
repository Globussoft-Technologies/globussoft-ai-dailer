package wshandler

import "math/rand"

// fillersByLang maps language codes to conversational filler words.
// These are injected as the first TTS item when the user speaks >2 words,
// giving a more natural response cadence while the LLM generates its reply.
var fillersByLang = map[string][]string{
	"hi": {"Hmm...", "Achha...", "Okay...", "Haan..."},
	"mr": {"Hmm...", "Achha...", "Theek ahe...", "Ho..."},
	"en": {"Hmm...", "I see...", "Okay...", "Right..."},
	"ta": {"Hmm...", "Sari...", "Okay...", "Aama..."},
	"te": {"Hmm...", "Sare...", "Okay...", "Avunu..."},
	"bn": {"Hmm...", "Achha...", "Okay...", "Haan..."},
	"gu": {"Hmm...", "Saru...", "Okay...", "Ha..."},
	"kn": {"Hmm...", "Sari...", "Okay...", "Houdu..."},
	"ml": {"Hmm...", "Sari...", "Okay...", "Athe..."},
	"pa": {"Hmm...", "Achha...", "Okay...", "Haan..."},
}

// defaultFillers is used when the language has no specific mapping.
var defaultFillers = []string{"Hmm...", "Okay...", "I see..."}

// randomFiller returns a random filler phrase for the given language code.
// Mirrors Python ws_handler.py:
//
//	fillers = ["Hmm...", "Achha...", "Okay...", "Haan..."]
//	await tts_queue.put(random.choice(fillers))
func randomFiller(language string) string {
	fillers, ok := fillersByLang[language]
	if !ok {
		fillers = defaultFillers
	}
	return fillers[rand.Intn(len(fillers))]
}
