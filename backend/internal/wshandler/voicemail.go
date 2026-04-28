package wshandler

import (
	"context"
	"strings"

	"go.uber.org/zap"
)

// voicemailPhrases is the substring trigger list — matched case-insensitively
// against final STT transcripts. Ported from main-branch ws_handler.py 4aa3fa3
// _VOICEMAIL_PHRASES. Order doesn't matter; the first hit wins.
var voicemailPhrases = []string{
	// English carrier voicemail
	"not available", "unavailable", "unable to take your call",
	"please leave a message", "leave a message", "leave your message",
	"please record your message", "record your message",
	"after the beep", "after the tone", "at the tone",
	"you have reached", "you've reached",
	"the number you have dialed", "the person you are trying",
	"this mailbox", "voicemail", "voice mail",
	"please press", "press 1", "press 0",
	"call back later", "try again later",
	// Hindi carrier voicemail
	"abhi uplabdh nahi", "sandesh chhodein", "beep ke baad",
	"is samay upalabdh nahi",
}

// isVoicemail reports whether `text` looks like a carrier voicemail greeting.
func isVoicemail(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, p := range voicemailPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// voicemailPitch returns a one-sentence pitch in the requested language.
// {leadFirst} and {agent} are interpolated; the company is intentionally
// generic ("humare team" etc.) since the Go session doesn't carry an
// explicit CompanyName today and caller ID covers identity.
func voicemailPitch(language, leadFirst, agentName string) string {
	if leadFirst == "" {
		leadFirst = "ji"
	}
	if agentName == "" {
		agentName = "Arjun"
	}
	switch language {
	case "bn":
		return "নমস্কার " + leadFirst + " জি, আমি " + agentName + ", আপনার enquiry সম্পর্কে কথা বলতে চেয়েছিলাম — সময় পেলে এই নম্বরে call back করুন। ধন্যবাদ।"
	case "mr":
		return "Namaskar " + leadFirst + " ji, mi " + agentName + " bolto — tumchya enquiry sandarbhat bolaaycha hota, vel zhala ki ya number var call back kara. Dhanyavad."
	case "ta":
		return "Vanakkam " + leadFirst + " sir, naan " + agentName + " — ungal enquiry pathi pesa virumbinaen, neram irundha intha number la call back pannunga. Nandri."
	case "te":
		return "Namaskaram " + leadFirst + " garu, nenu " + agentName + " — mee enquiry gurinchi maatladaalanukunna, samayam unnappudu ee number ki call back cheyyandi. Dhanyavadalu."
	case "kn":
		return "Namaskara " + leadFirst + " sir, naanu " + agentName + " — nimma enquiry bagge maathaada bayasiddenu, samaya sigthare ee number ge call back maadi. Dhanyavaada."
	case "ml":
		return "Namaskaram " + leadFirst + " sir, njan " + agentName + " — ningalude enquiry pattiyaanu vilichathu, samayam kittumbol ee number il call back cheyyu. Nanni."
	case "gu":
		return "Namaste " + leadFirst + " ji, hu " + agentName + " — tamari enquiry vishe vat karva mate phone karyo to, samay made to aa number par call back karjo. Aabhar."
	case "pa":
		return "Sat sri akal " + leadFirst + " ji, main " + agentName + " — tuhadi enquiry baare gal karni si, vela mile te is number te call back karo ji. Dhanvaad."
	case "en":
		return "Hi " + leadFirst + ", this is " + agentName + " — I was calling regarding your enquiry, please call us back at your convenience. Thank you."
	default: // hi and any unrecognized
		return "Namaste " + leadFirst + " ji, main " + agentName + " bol raha tha — aapki enquiry ke baare mein baat karni thi, samay mile to is number par call back karein. Dhanyavad."
	}
}

// handleVoicemail is the response when isVoicemail() fires on a transcript.
// It cancels any in-flight TTS, queues a single pitch sentence, and signals
// the TTS worker to drain + close (the empty-string HANGUP sentinel).
// processTranscript should `return` immediately after invoking this — no LLM,
// no further STT processing for this call.
func handleVoicemail(ctx context.Context, sess *CallSession, transcript string) {
	if sess == nil {
		return
	}
	sess.Log.Info("[VOICEMAIL] detected — leaving pitch and hanging up",
		zap.String("transcript", truncate(transcript, 80)),
		zap.String("language", sess.Language),
		zap.Int64("lead_id", sess.LeadID))

	// Cancel any TTS already in flight (e.g. greeting still playing).
	sess.CancelActiveTTS()

	pitch := voicemailPitch(sess.Language, firstWord(sess.LeadName), sess.AgentName)
	sess.RequestHangup()

	// Push pitch + HANGUP sentinel onto the TTS queue. runTTSWorker handles
	// drain + WebSocket close after the empty-string sentinel.
	select {
	case sess.TTSSentences <- pitch:
	case <-ctx.Done():
		return
	}
	select {
	case sess.TTSSentences <- "":
	case <-ctx.Done():
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}
