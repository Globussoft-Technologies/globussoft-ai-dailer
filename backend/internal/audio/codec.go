// Package audio handles PCM/ulaw codec operations and resampling.
// Replaces Python's audioop.ulaw2lin, audioop.lin2ulaw, and audioop.ratecv.
package audio

import "github.com/zaf/g711"

// UlawToPCM converts 8-bit mu-law bytes to 16-bit linear PCM bytes (little-endian).
// Equivalent to: audioop.ulaw2lin(data, 2)
// g711.DecodeUlaw accepts a ulaw buffer and returns a PCM buffer.
func UlawToPCM(ulaw []byte) []byte {
	return g711.DecodeUlaw(ulaw)
}

// PCMToUlaw converts 16-bit linear PCM bytes (little-endian) to 8-bit mu-law bytes.
// Equivalent to: audioop.lin2ulaw(data, 2)
// g711.EncodeUlaw accepts a PCM buffer and returns a ulaw buffer.
func PCMToUlaw(pcm []byte) []byte {
	return g711.EncodeUlaw(pcm)
}

// Decimate2x downsamples 16kHz 16-bit PCM to 8kHz by dropping every other sample.
// Equivalent to: audioop.ratecv(data, 2, 1, 16000, 8000, None)
// Input must have an even number of bytes (pairs of int16 samples at 16kHz).
func Decimate2x(pcm16k []byte) []byte {
	samples := len(pcm16k) / 2 // total 16-bit samples at 16kHz
	outSamples := samples / 2  // keep every 2nd sample → 8kHz
	out := make([]byte, outSamples*2)
	for i := range outSamples {
		// Source sample index i*2 (take every other sample)
		src := i * 4
		out[i*2] = pcm16k[src]
		out[i*2+1] = pcm16k[src+1]
	}
	return out
}
