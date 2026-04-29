import React, { useState, useRef } from 'react';

export default function CallMonitor({ apiUrl }) {
  const [streamSid, setStreamSid] = useState('');
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState('');
  const [transcripts, setTranscripts] = useState([]);
  const [whisperText, setWhisperText] = useState('');
  const [takeoverActive, setTakeoverActive] = useState(false);
  const wsRef = useRef(null);

  const connectToCall = () => {
    setError('');
    const sid = streamSid.trim();
    if (!sid) {
      setError('Stream SID is required');
      return;
    }
    setConnecting(true);
    const wsUrl = apiUrl.replace("http", "ws") + `/ws/monitor/${sid}`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    // Track open status in the closure so onclose can distinguish
    // "server rejected the SID" (close before open) from a normal disconnect.
    let opened = false;

    ws.onopen = () => {
      opened = true;
      setConnecting(false);
      setConnected(true);
    };
    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      // Server may upgrade the WS and then immediately send {"error": "..."}
      // (e.g. session not found). Surface it instead of silently dropping.
      if (data.error) {
        setError(data.error);
        ws.close();
        return;
      }
      if (data.type === 'transcript') {
        setTranscripts(prev => [...prev, data]);
      }
    };
    ws.onclose = () => {
      if (!opened) {
        setError(`No active stream found for SID "${sid}"`);
      }
      setConnecting(false);
      setConnected(false);
    };
    ws.onerror = () => {
      if (!opened) {
        setError('Could not connect to monitor stream. Check the SID and try again.');
      }
    };
  };

  const sendWhisper = () => {
    if (wsRef.current && whisperText) {
      wsRef.current.send(JSON.stringify({ action: 'whisper', text: whisperText }));
      setTranscripts(prev => [...prev, { role: 'system', text: `Whisper sent: ${whisperText}` }]);
      setWhisperText('');
    }
  };

  const toggleTakeover = async () => {
    if (!takeoverActive) {
      if (wsRef.current) {
        wsRef.current.send(JSON.stringify({ action: 'takeover' }));
        setTakeoverActive(true);
        setTranscripts(prev => [...prev, { role: 'system', text: 'Call Takeover Active. You are now speaking.' }]);

        try {
          const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
          // Note for demo: Raw web audio requires mu-law encoding for Twilio compatibility.
          // In full production, we'd use an AudioWorkletProcessor here.
        } catch (e) {
          console.error("Mic access denied");
        }
      }
    }
  };

  return (
    <div className="glass-panel" style={{padding: '2rem'}}>
      <h2 style={{marginTop: 0, marginBottom: '1.5rem', color: '#f8fafc'}}>🎙️ Live Call Monitor</h2>
      <p style={{color: '#94a3b8', marginBottom: '2rem'}}>Inject dynamic instructions into the AI's mind instantly, or take over the line if the client demands human interaction.</p>
      
      {!connected ? (
        <>
          {error && (
            <div style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#fca5a5', fontSize: '0.85rem'}}>
              {error}
            </div>
          )}
          <form
            onSubmit={(e) => { e.preventDefault(); connectToCall(); }}
            style={{display: 'flex', gap: '1rem', alignItems: 'center'}}
          >
            <input
              className="form-input"
              placeholder="Enter active Stream SID routing ID..."
              value={streamSid}
              onChange={(e) => { setStreamSid(e.target.value); if (error) setError(''); }}
              required
              disabled={connecting}
              style={{flex: 1, marginBottom: 0}}
            />
            <button
              type="submit"
              className="btn-primary"
              disabled={connecting || !streamSid.trim()}
            >
              {connecting ? 'Connecting…' : 'Connect Monitor'}
            </button>
          </form>
        </>
      ) : (
        <div style={{display: 'flex', flexDirection: 'column', gap: '1.5rem'}}>
          <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center'}}>
            <div><span className="badge" style={{background: 'rgba(34,197,94,0.1)', color: '#4ade80'}}>Connected to {streamSid}</span></div>
            <button className="btn-call" style={{borderColor: '#ef4444', color: '#ef4444'}} onClick={() => wsRef.current?.close()}>Disconnect</button>
          </div>
          
          <div style={{background: 'rgba(15, 23, 42, 0.6)', border: '1px solid rgba(255,255,255,0.05)', borderRadius: '8px', padding: '1.5rem', height: '350px', overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '12px'}}>
            {transcripts.length === 0 && <p style={{color: '#64748b', textAlign: 'center'}}>Waiting for speech...</p>}
            {transcripts.map((t, idx) => (
              <div key={idx} style={{
                alignSelf: t.role === 'user' ? 'flex-start' : t.role === 'system' ? 'center' : 'flex-end',
                background: t.role === 'user' ? 'rgba(56, 189, 248, 0.1)' : t.role === 'system' ? 'rgba(234, 179, 8, 0.1)' : 'rgba(168, 85, 247, 0.1)',
                padding: '10px 16px',
                borderRadius: '8px',
                color: t.role === 'user' ? '#e0f2fe' : t.role === 'system' ? '#fde047' : '#f3e8ff',
                maxWidth: '80%',
                fontSize: t.role === 'system' ? '0.8rem' : '0.95rem',
                border: `1px solid ${t.role === 'user' ? 'rgba(56, 189, 248, 0.2)' : 'rgba(168, 85, 247, 0.2)'}`
              }}>
                <strong style={{display: 'block', fontSize: '0.75rem', textTransform: 'uppercase', marginBottom: '4px', opacity: 0.7}}>{t.role === 'user' ? 'Lead' : t.role === 'system' ? 'System' : 'AI Agent'}</strong>
                {t.text}
              </div>
            ))}
          </div>

          <div style={{display: 'flex', gap: '1rem'}}>
            <input 
              className="form-input" 
              placeholder="Type a whisper instruction to the AI (e.g. 'Offer 5% discount now')..." 
              value={whisperText} 
              onChange={e => setWhisperText(e.target.value)}
              style={{flex: 1, marginBottom: 0}}
              disabled={takeoverActive}
            />
            <button className="btn-call" style={{background: 'rgba(56, 189, 248, 0.15)', color: '#38bdf8'}} onClick={sendWhisper} disabled={takeoverActive}>💬 Whisper</button>
            <button className="btn-call" style={{background: takeoverActive ? 'rgba(239, 68, 68, 0.9)' : 'rgba(239, 68, 68, 0.15)', color: takeoverActive ? 'white' : '#ef4444'}} onClick={toggleTakeover}>
              {takeoverActive ? '🎤 Taking Over' : '🚨 Takeover Call'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
