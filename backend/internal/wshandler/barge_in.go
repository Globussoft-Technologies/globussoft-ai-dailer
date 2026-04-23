package wshandler

import "encoding/json"

// sendClearEvent sends {"event":"clear","streamSid":"..."} to the phone.
// This clears Exotel's audio playback buffer when the user starts speaking
// mid-TTS (barge-in). Must be called after CancelActiveTTS.
func sendClearEvent(sess *CallSession) {
	frame, _ := json.Marshal(map[string]string{
		"event":     "clear",
		"streamSid": sess.StreamSid,
	})
	_ = sess.SendText(frame)
}
