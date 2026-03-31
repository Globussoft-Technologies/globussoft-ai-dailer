import React, { useState, useEffect, useRef } from 'react';

export default function LogsTab({ API_URL, authToken }) {
  const [mode, setMode] = useState('activity'); // 'activity' or 'verbose'
  const [filter, setFilter] = useState('');
  const [paused, setPaused] = useState(false);
  const [activityLogs, setActivityLogs] = useState([]);
  const verboseRef = useRef(null);
  const activityEsRef = useRef(null);
  const verboseEsRef = useRef(null);

  // Activity feed (user-friendly campaign events)
  useEffect(() => {
    if (activityEsRef.current) activityEsRef.current.close();
    const es = new EventSource(`${API_URL}/campaign-events?token=${authToken}&campaign_id=0`);
    es.onmessage = (ev) => {
      if (!paused) {
        setActivityLogs(prev => [...prev.slice(-200), ev.data]);
      }
    };
    activityEsRef.current = es;
    return () => es.close();
  }, [paused]);

  // Verbose feed (technical server logs)
  useEffect(() => {
    if (mode !== 'verbose' || !verboseRef.current) return;
    if (verboseEsRef.current) verboseEsRef.current.close();
    const el = verboseRef.current;
    el.innerHTML = '';
    const es = new EventSource(`${API_URL}/live-logs?token=${authToken}`);
    es.onmessage = (ev) => {
      if (paused) return;
      if (filter && !ev.data.toLowerCase().includes(filter.toLowerCase())) return;
      const line = document.createElement('div');
      line.textContent = ev.data;
      line.style.padding = '3px 12px';
      line.style.fontFamily = '"JetBrains Mono", "Fira Code", monospace';
      line.style.fontSize = '0.75rem';
      line.style.borderBottom = '1px solid rgba(255,255,255,0.03)';
      line.style.lineHeight = '1.4';
      if (ev.data.includes('ERROR')) { line.style.color = '#f87171'; line.style.background = 'rgba(239,68,68,0.06)'; }
      else if (ev.data.includes('WARNING')) { line.style.color = '#fbbf24'; }
      else if (ev.data.includes('[STT]')) { line.style.color = '#4ade80'; }
      else if (ev.data.includes('[LLM]')) { line.style.color = '#67e8f9'; }
      else if (ev.data.includes('TTS')) { line.style.color = '#a78bfa'; }
      else if (ev.data.includes('GREETING') || ev.data.includes('RECORDING')) { line.style.color = '#f59e0b'; }
      else if (ev.data.includes('DIAL') || ev.data.includes('EXOTEL')) { line.style.color = '#60a5fa'; }
      else if (ev.data.includes('HANGUP') || ev.data.includes('CLOSED')) { line.style.color = '#fb923c'; }
      else if (ev.data.includes('DEBUG-REC')) { line.style.color = '#22d3ee'; }
      else { line.style.color = '#64748b'; }
      el.appendChild(line);
      if (el.children.length > 500) el.removeChild(el.firstChild);
      el.scrollTop = el.scrollHeight;
    };
    verboseEsRef.current = es;
    return () => es.close();
  }, [mode, paused, filter]);

  const activityIcon = (text) => {
    if (text.includes('📞')) return { bg: 'rgba(96,165,250,0.1)', border: 'rgba(96,165,250,0.2)' };
    if (text.includes('✅') || text.includes('🎯')) return { bg: 'rgba(34,197,94,0.1)', border: 'rgba(34,197,94,0.2)' };
    if (text.includes('❌')) return { bg: 'rgba(245,158,11,0.1)', border: 'rgba(245,158,11,0.2)' };
    if (text.includes('📵') || text.includes('⚠️') || text.includes('💥')) return { bg: 'rgba(239,68,68,0.1)', border: 'rgba(239,68,68,0.2)' };
    if (text.includes('🚀') || text.includes('🏁')) return { bg: 'rgba(139,92,246,0.1)', border: 'rgba(139,92,246,0.2)' };
    return { bg: 'rgba(255,255,255,0.03)', border: 'rgba(255,255,255,0.05)' };
  };

  return (
    <div style={{padding: '1rem'}}>
      {/* Header */}
      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '10px'}}>
        <h2 style={{margin: 0}}>📡 System Logs</h2>
        <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
          {/* Mode Toggle */}
          <div style={{display: 'flex', borderRadius: '8px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)'}}>
            <button onClick={() => setMode('activity')}
              style={{padding: '6px 16px', border: 'none', cursor: 'pointer', fontSize: '0.8rem', fontWeight: 600,
                background: mode === 'activity' ? 'rgba(34,197,94,0.2)' : 'transparent',
                color: mode === 'activity' ? '#22c55e' : '#64748b'}}>
              📋 Activity
            </button>
            <button onClick={() => setMode('verbose')}
              style={{padding: '6px 16px', border: 'none', cursor: 'pointer', fontSize: '0.8rem', fontWeight: 600,
                background: mode === 'verbose' ? 'rgba(96,165,250,0.2)' : 'transparent',
                color: mode === 'verbose' ? '#60a5fa' : '#64748b'}}>
              🔧 Verbose
            </button>
          </div>

          {/* Filter (verbose only) */}
          {mode === 'verbose' && (
            <input className="form-input" placeholder="Filter logs..." value={filter}
              onChange={e => setFilter(e.target.value)}
              style={{width: '160px', height: '30px', fontSize: '0.8rem', padding: '4px 8px'}} />
          )}

          {/* Pause/Resume */}
          <button onClick={() => setPaused(!paused)}
            style={{padding: '6px 12px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)',
              background: paused ? 'rgba(239,68,68,0.15)' : 'rgba(34,197,94,0.15)',
              color: paused ? '#ef4444' : '#22c55e', cursor: 'pointer', fontSize: '0.8rem', fontWeight: 600}}>
            {paused ? '⏸ Paused' : '▶ Live'}
          </button>

          {/* Clear */}
          <button onClick={() => {
            setActivityLogs([]);
            if (verboseRef.current) verboseRef.current.innerHTML = '';
          }}
            style={{padding: '6px 12px', borderRadius: '6px', border: '1px solid rgba(239,68,68,0.2)',
              background: 'rgba(239,68,68,0.1)', color: '#fca5a5', cursor: 'pointer', fontSize: '0.8rem'}}>
            🗑️ Clear
          </button>
        </div>
      </div>

      {/* Legend */}
      {mode === 'verbose' && (
        <div style={{display: 'flex', gap: '12px', marginBottom: '10px', fontSize: '0.7rem', flexWrap: 'wrap'}}>
          <span style={{color: '#4ade80'}}>● STT</span>
          <span style={{color: '#67e8f9'}}>● LLM</span>
          <span style={{color: '#a78bfa'}}>● TTS</span>
          <span style={{color: '#60a5fa'}}>● DIAL</span>
          <span style={{color: '#f59e0b'}}>● GREETING/REC</span>
          <span style={{color: '#fb923c'}}>● HANGUP</span>
          <span style={{color: '#22d3ee'}}>● DEBUG</span>
          <span style={{color: '#f87171'}}>● ERROR</span>
        </div>
      )}

      {/* Activity Mode */}
      {mode === 'activity' && (
        <div style={{background: 'rgba(2,6,23,0.8)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: '8px', height: '70vh', overflowY: 'auto', padding: '8px'}}>
          {activityLogs.length === 0 ? (
            <div style={{textAlign: 'center', color: '#64748b', padding: '3rem'}}>
              <div style={{fontSize: '2rem', marginBottom: '12px'}}>📡</div>
              <div>Waiting for campaign activity...</div>
              <div style={{fontSize: '0.8rem', marginTop: '8px'}}>Start a campaign dial to see live events here.</div>
            </div>
          ) : (
            activityLogs.map((log, i) => {
              const style = activityIcon(log);
              return (
                <div key={i} style={{
                  padding: '8px 12px', marginBottom: '4px', borderRadius: '6px',
                  background: style.bg, borderLeft: `3px solid ${style.border}`,
                  fontSize: '0.85rem', color: '#e2e8f0', fontFamily: 'system-ui'
                }}>
                  {log}
                </div>
              );
            })
          )}
        </div>
      )}

      {/* Verbose Mode */}
      {mode === 'verbose' && (
        <div ref={verboseRef} style={{
          background: 'rgba(2,6,23,0.8)', border: '1px solid rgba(255,255,255,0.06)',
          borderRadius: '8px', height: '70vh', overflowY: 'auto', overflowX: 'hidden'
        }} />
      )}
    </div>
  );
}
