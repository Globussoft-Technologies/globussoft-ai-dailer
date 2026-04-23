/**
 * Local dev: Vite serves the UI on :5173; FastAPI usually runs on :8000.
 * WebSockets cannot use the /api proxy path, so the browser opens ws://host:PORT/... directly.
 * Override in frontend/.env: VITE_BACKEND_PORT=8001
 */
export const BACKEND_PORT = import.meta.env.VITE_BACKEND_PORT ?? '8000';

/**
 * Sim Web call: TTS plays on speakers while the mic captures the user on the same machine.
 * Default echo cancellation often treats the user's voice as echo of the AI and sends silence
 * to the server — Deepgram then sees only zeros and never triggers replies after the greeting.
 */
export const SIM_WEB_AUDIO_CONSTRAINTS = {
  echoCancellation: false,
  noiseSuppression: true,
  autoGainControl: true,
};

/**
 * Deepgram + recording expect linear16 at 8 kHz.
 *
 * Important: use a normal `new AudioContext()` (typically 44.1 / 48 kHz) and **downsample** to 8 kHz
 * before sending. `new AudioContext({ sampleRate: 8000 })` often makes `MediaStreamSource` →
 * `ScriptProcessor` produce **all-zero samples** (base64 payloads that look like long `AAAA…` runs),
 * so Deepgram only sees silence and you never get `stt_interim` / `user_speech`.
 */
export const SIM_WEB_MIC_SAMPLE_RATE = 8000;

/** RMS of mono float32 samples in [-1, 1]. */
export function simWebRmsFloat32(buf) {
  if (!buf || buf.length === 0) return 0;
  let s = 0;
  for (let i = 0; i < buf.length; i++) s += buf[i] * buf[i];
  return Math.sqrt(s / buf.length);
}

/**
 * Detect sustained silence on the **8 kHz** float32 buffer we send to the server.
 * `state` = { streak: number, warned: boolean } — mutate in place.
 */
export function simWebTrackMicSilence(float32Array, state) {
  const rms = simWebRmsFloat32(float32Array);
  const silent = rms < 0.00008;
  if (silent) {
    state.streak = (state.streak || 0) + 1;
    if (state.streak >= 50 && !state.warned) {
      state.warned = true;
      simWebAlways(
        'WARNING: outgoing mic PCM is silent (near zero). STT cannot work. ' +
          'Confirm OS mic + browser input; avoid forcing AudioContext sampleRate to 8000 (use default + downsample).'
      );
    }
  } else {
    state.streak = 0;
  }
  return rms;
}

/** Wait this long after the last TTS sample finishes playing before sending mic to STT (echo guard). */
export const SIM_WEB_UNMUTE_AFTER_MS = 500;

/** Log every Nth mic→server media frame as `[SimWeb] send media …` (matches backend STT chunk cadence). */
export const SIM_WEB_SEND_MEDIA_LOG_EVERY = 30;

/** Log every Nth inbound TTS PCM chunk as `[SimWeb] recv media …` (avoids console flood). */
export const SIM_WEB_RECV_TTS_LOG_EVERY = 5;

/**
 * Server-side (Deepgram) silence before an utterance is finalized for browser_sim — must match `ws_handler.py`.
 * After this much quiet audio, STT sends `user_speech` → LLM → TTS (continuous back-and-forth).
 */
export const SIM_WEB_STT_SILENCE_MS = 1000;

export function downsampleFloat32To8kHz(input, inputSampleRate) {
  if (inputSampleRate === SIM_WEB_MIC_SAMPLE_RATE) return input;
  const ratio = inputSampleRate / SIM_WEB_MIC_SAMPLE_RATE;
  const outLen = Math.max(1, Math.floor(input.length / ratio));
  const out = new Float32Array(outLen);
  for (let i = 0; i < outLen; i++) {
    const srcPos = i * ratio;
    const i0 = Math.floor(srcPos);
    const i1 = Math.min(i0 + 1, input.length - 1);
    const t = srcPos - i0;
    out[i] = input[i0] * (1 - t) + input[i1] * t;
  }
  return out;
}

/** Set localStorage SIM_WEB_DEBUG=1 for RMS and extra detail. */
export function simWebLog(...args) {
  const verbose =
    typeof localStorage !== 'undefined' && localStorage.getItem('SIM_WEB_DEBUG') === '1';
  const tag = args[0];
  if (verbose) {
    console.log('[SimWeb]', ...args);
    return;
  }
  if (
    tag === 'session' ||
    tag === 'mute' ||
    tag === 'unmute' ||
    tag === 'WS' ||
    tag === 'mic' ||
    tag === 'tts' ||
    tag === 'send' ||
    tag === 'recv'
  ) {
    console.log('[SimWeb]', ...args);
  }
}

/** Always printed — use for one-shot lifecycle lines. */
export function simWebAlways(...args) {
  console.log('[SimWeb]', ...args);
}

/** Avoid InvalidStateError when the same AudioContext is closed twice (HMR, end-call, etc.). */
export function safeCloseAudioContext(ctx) {
  if (!ctx) return;
  try {
    if (ctx.state !== 'closed') ctx.close();
  } catch {
    /* already closed */
  }
}
