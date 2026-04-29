import React, { useState, useRef } from 'react';

const VOICE_OPTIONS = {
  elevenlabs: {
    label: 'ElevenLabs',
    voices: [
      { id: 'oH8YmZXJYEZq5ScgoGn9', name: 'Aakash – Friendly Customer Support' },
      { id: 'X4ExprIXDKrWcHdtGysh', name: 'Anjura – Confident & Energetic' },
      { id: 'SXuKWBhKoIoAHKlf6Gt3', name: 'Gaurav – Professional Indian English' },
      { id: 'N09NFwYJJG9VSSgdLQbT', name: 'Ishan – Bold & Upbeat' },
      { id: 'U9wNM2BNANqtBCawWLgA', name: 'Himanshu – Calm & Serene' },
      { id: 'h061KGyOtpLYDxcoi8E3', name: 'Ravi – Gentle & Informative' },
      { id: 'Ock0AL5DBkvTUDePt4Hm', name: 'Viraj – Bold & Commanding' },
      { id: 'nwj0s2LU9bDWRKND5yzA', name: 'Bunty – Energetic & Fun' },
      { id: 'amiAXapsDOAiHJqbsAZj', name: 'Priya – Confident Female' },
      { id: '6JsmTroalVewG1gA6Jmw', name: 'Sia – Friendly Conversational' },
      { id: '9vP6R7VVxNwGIGLnpl17', name: 'Suhana – Young & Joyful' },
      { id: 'hO2yZ8lxM3axUxL8OeKX', name: 'Mini – Lively & Cute' },
      { id: 's0oIsoSJ9raiUm7DJNzW', name: '⭐ Current Default Voice' },
    ]
  },
  smallest: {
    label: 'Smallest AI',
    voices: [
      { id: 'mithali', name: 'Mithali (Hindi Female)' },
      { id: 'priya', name: 'Priya (Hindi Female)' },
      { id: 'aravind', name: 'Aravind (Hindi Male)' },
      { id: 'raj', name: 'Raj (Hindi Male)' },
      { id: 'arman', name: 'Arman (Male)' },
      { id: 'jasmine', name: 'Jasmine (Female)' },
      { id: 'emily', name: 'Emily (Female)' },
      { id: 'james', name: 'James (Male)' },
    ]
  }
};

export default function Sandbox({ apiUrl }) {
  const [recording, setRecording] = useState(false);
  const [transcripts, setTranscripts] = useState([]);
  const [provider, setProvider] = useState('elevenlabs');
  const [voiceId, setVoiceId] = useState('oH8YmZXJYEZq5ScgoGn9');
  const wsRef = useRef(null);
  const audioContextRef = useRef(null);
  const sourceRef = useRef(null);
  const processorRef = useRef(null);

  const handleProviderChange = (p) => {
    setProvider(p);
    setVoiceId(VOICE_OPTIONS[p].voices[0].id);
  };

  const startSandbox = async () => {
    try {
      // Default getUserMedia({audio: true}) silently enables aggressive echo
      // cancellation, noise suppression, and AGC. With the AI's TTS playing
      // through the speakers, AEC was treating the user's own voice as echo
      // and attenuating it — Deepgram saw audio energy of ~90 (near silence)
      // even when the user spoke clearly. Turning the processing off here
      // makes the mic pass through speech at normal levels. (issue #33)
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: false,
          noiseSuppression: false,
          autoGainControl: false,
        },
      });
      // 16 kHz is well-supported across browsers. We then downsample by 2 to
      // 8 kHz before sending so the bytes match what Deepgram expects
      // (sample_rate=8000, encoding=linear16). Asking the browser directly for
      // 8 kHz didn't work reliably — Chrome silently fell back to 48 kHz on
      // macOS while still labelling the buffer as 8 kHz, producing audio that
      // Deepgram couldn't recognise (transcripts came back with confidence 0).
      // (issue #33)
      const audioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 16000 });
      audioContextRef.current = audioContext;

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.hostname;
      const qp = new URLSearchParams({
        name: 'Sandbox Tester',
        interest: 'product demo',
        lead_id: '0',
        tts_provider: provider,
        voice: voiceId,
        // "multi" tells the backend to put Deepgram in multi-language mode so
        // STT works for whichever language the tester speaks (English, Hindi,
        // Tamil, etc). Without this it would default to English-only and
        // return empty transcripts for any non-English speech. (issue #33)
        tts_language: 'multi',
      }).toString();

      let wsUrl;
      if (host === 'localhost' || host === '127.0.0.1') {
        wsUrl = `ws://${host}:8001/media-stream?${qp}`;
      } else {
        wsUrl = `${protocol}//${window.location.host}/media-stream?${qp}`;
      }

      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        setRecording(true);
        ws.send(JSON.stringify({ event: 'connected' }));
        const sid = `web_sim_sandbox_${Date.now()}`;
        ws.send(JSON.stringify({ event: 'start', start: { stream_sid: sid }, stream_sid: sid }));

        const source = audioContext.createMediaStreamSource(stream);
        sourceRef.current = source;
        const processor = audioContext.createScriptProcessor(2048, 1, 1);
        processorRef.current = processor;
        source.connect(processor);
        processor.connect(audioContext.destination);

        let micMuted = true;
        let unmuteTimer = null;

        processor.onaudioprocess = (e) => {
          if (ws.readyState !== WebSocket.OPEN || micMuted) return;
          const float32Array = e.inputBuffer.getChannelData(0);
          // Downsample 16 kHz → 8 kHz by taking every other sample. Speech
          // energy is well below 4 kHz so a low-pass filter isn't strictly
          // necessary; the alias headroom is fine for STT.
          const outLen = Math.floor(float32Array.length / 2);
          const int16Buffer = new Int16Array(outLen);
          for (let i = 0; i < outLen; i++) {
            let s = Math.max(-1, Math.min(1, float32Array[i * 2]));
            int16Buffer[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
          }
          let binary = '';
          const bytes = new Uint8Array(int16Buffer.buffer);
          for (let i = 0; i < bytes.byteLength; i++) {
            binary += String.fromCharCode(bytes[i]);
          }
          ws.send(JSON.stringify({ event: 'media', media: { payload: window.btoa(binary) } }));
        };

        let nextPlayTime = audioContext.currentTime;
        ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          if (data.event === 'media') {
            micMuted = true;
            if (unmuteTimer) clearTimeout(unmuteTimer);
            const audioStr = window.atob(data.media.payload);
            const audioBytes = new Uint8Array(audioStr.length);
            for (let i = 0; i < audioStr.length; i++) audioBytes[i] = audioStr.charCodeAt(i);
            const int16Array = new Int16Array(audioBytes.buffer);
            const float32Array = new Float32Array(int16Array.length);
            for (let i = 0; i < int16Array.length; i++) float32Array[i] = int16Array[i] / 0x8000;
            const audioBuffer = audioContext.createBuffer(1, float32Array.length, 8000);
            audioBuffer.copyToChannel(float32Array, 0);
            const bufferSource = audioContext.createBufferSource();
            bufferSource.buffer = audioBuffer;
            bufferSource.connect(audioContext.destination);
            const now = audioContext.currentTime;
            const startAt = Math.max(now, nextPlayTime);
            bufferSource.start(startAt);
            nextPlayTime = startAt + audioBuffer.duration;
            unmuteTimer = setTimeout(() => { micMuted = false; }, (nextPlayTime - now) * 1000 + 200);
          } else if (data.type === 'transcript') {
            // Backend sends {type:"transcript", role:"user"|"agent", text}.
            // Map "agent" → "assistant" for the existing render styling.
            const role = data.role === 'agent' ? 'assistant' : data.role;
            if (role && data.text) setTranscripts(prev => [...prev, { role, text: data.text }]);
          }
        };
      };

      ws.onerror = (e) => console.error("Sandbox WS error", e);
      ws.onclose = () => setRecording(false);
    } catch (e) {
      console.error("Sandbox failed", e);
      alert("Microphone access required. Please allow and retry.");
    }
  };

  const stopSandbox = () => {
    setRecording(false);
    if (processorRef.current && sourceRef.current) {
      sourceRef.current.disconnect();
      processorRef.current.disconnect();
    }
    if (wsRef.current) wsRef.current.close();
  };

  const currentVoices = VOICE_OPTIONS[provider]?.voices || [];
  const selectedVoiceName = currentVoices.find(v => v.id === voiceId)?.name || '';

  return (
    <div className="glass-panel" style={{padding: '2rem'}}>
      <h2 style={{marginTop: 0, marginBottom: '0.5rem', color: '#f8fafc'}}>🎯 AI Training Sandbox</h2>
      <p style={{color: '#94a3b8', marginBottom: '2rem'}}>Roleplay and stress test the Voice Agent engine. Choose different TTS providers and voices to find the best fit.</p>
      
      <div style={{display: 'flex', gap: '2rem'}}>
        {/* Left Panel */}
        <div style={{background: 'rgba(15, 23, 42, 0.6)', borderRadius: '8px', padding: '1.5rem', flex: 1}}>
          <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Simulation Controls</h3>
          
          {/* Provider Selector */}
          <div style={{marginBottom: '1rem'}}>
            <label style={{display: 'block', fontSize: '0.8rem', color: '#64748b', fontWeight: 600, marginBottom: '6px', textTransform: 'uppercase', letterSpacing: '0.05em'}}>TTS Provider</label>
            <div style={{display: 'flex', gap: '8px'}}>
              {Object.entries(VOICE_OPTIONS).map(([key, val]) => (
                <button key={key} onClick={() => handleProviderChange(key)} disabled={recording}
                  style={{flex: 1, padding: '8px 12px', borderRadius: '8px', cursor: recording ? 'not-allowed' : 'pointer', fontWeight: 600, fontSize: '0.85rem', border: 'none',
                    background: provider === key ? 'linear-gradient(135deg, #8b5cf6, #6d28d9)' : 'rgba(255,255,255,0.05)',
                    color: provider === key ? '#fff' : '#94a3b8', opacity: recording ? 0.5 : 1,
                    transition: 'all 0.2s'}}>
                  {val.label}
                </button>
              ))}
            </div>
          </div>

          {/* Voice Selector */}
          <div style={{marginBottom: '1.5rem'}}>
            <label style={{display: 'block', fontSize: '0.8rem', color: '#64748b', fontWeight: 600, marginBottom: '6px', textTransform: 'uppercase', letterSpacing: '0.05em'}}>Voice</label>
            <select className="form-input" value={voiceId} onChange={e => setVoiceId(e.target.value)} disabled={recording}
              style={{width: '100%', fontSize: '0.9rem', padding: '10px 12px', opacity: recording ? 0.5 : 1}}>
              {currentVoices.map(v => (
                <option key={v.id} value={v.id}>{v.name}</option>
              ))}
            </select>
          </div>

          {/* Action Buttons */}
          <div style={{display: 'flex', gap: '1rem'}}>
            {!recording ? (
              <button className="btn-primary" onClick={startSandbox}
                style={{background: 'linear-gradient(135deg, #22c55e, #16a34a)', borderColor: '#22c55e', flex: 1}}>
                🎙️ Start Simulation
              </button>
            ) : (
              <button className="btn-call" onClick={stopSandbox}
                style={{borderColor: '#ef4444', color: '#ef4444', flex: 1}}>
                ⏹️ Stop
              </button>
            )}
            <button className="btn-call" onClick={() => setTranscripts([])} style={{flex: 0, fontSize: '0.8rem', whiteSpace: 'nowrap'}}>🗑️ Clear</button>
          </div>
          
          {/* Status */}
          <div style={{background: 'rgba(0,0,0,0.3)', borderRadius: '8px', padding: '1rem', marginTop: '1rem'}}>
            <div style={{display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', color: '#94a3b8', lineHeight: 1.8}}>
              <span>Mic:</span>
              <span>{recording ? <span style={{color: '#34d399'}}>Active 🟢</span> : <span style={{color: '#ef4444'}}>Off 🔴</span>}</span>
            </div>
            <div style={{display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', color: '#94a3b8', lineHeight: 1.8}}>
              <span>Provider:</span>
              <span style={{color: '#c4b5fd'}}>{VOICE_OPTIONS[provider].label}</span>
            </div>
            <div style={{display: 'flex', justifyContent: 'space-between', fontSize: '0.85rem', color: '#94a3b8', lineHeight: 1.8}}>
              <span>Voice:</span>
              <span style={{color: '#c4b5fd'}}>{selectedVoiceName}</span>
            </div>
          </div>
        </div>

        {/* Right Panel - Transcripts */}
        <div style={{background: 'rgba(15, 23, 42, 0.6)', borderRadius: '8px', padding: '1.5rem', flex: 2}}>
          <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Live Transcripts</h3>
          <div style={{background: 'rgba(0,0,0,0.3)', borderRadius: '8px', padding: '1.5rem', minHeight: '350px', maxHeight: '450px', display: 'flex', flexDirection: 'column', gap: '12px', overflowY: 'auto'}}>
            {transcripts.length === 0 && <p style={{color: '#64748b', textAlign: 'center', marginTop: '5rem'}}>Click "Start Simulation" and speak...</p>}
            {transcripts.map((t, idx) => (
              <div key={idx} style={{
                alignSelf: t.role === 'user' ? 'flex-start' : 'flex-end',
                background: t.role === 'user' ? 'rgba(56, 189, 248, 0.1)' : 'rgba(168, 85, 247, 0.1)',
                padding: '10px 16px', borderRadius: '12px',
                color: t.role === 'user' ? '#e0f2fe' : '#f3e8ff',
                maxWidth: '80%', border: `1px solid ${t.role === 'user' ? 'rgba(56, 189, 248, 0.2)' : 'rgba(168, 85, 247, 0.2)'}`
              }}>
                <strong style={{display: 'block', fontSize: '0.7rem', textTransform: 'uppercase', marginBottom: '4px', opacity: 0.6}}>
                  {t.role === 'user' ? '👤 You' : '🤖 AI Agent'}
                </strong>
                {t.text}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
