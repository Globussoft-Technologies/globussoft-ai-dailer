import React, { useEffect } from 'react';
import { formatDateTime } from '../../utils/dateFormat';
import AuthAudio from '../AuthAudio';

// Language code → display name. Covers the 10 languages the dialer supports.
// We show this as a small badge on each call header so you can tell at a
// glance which language the call was conducted in — the transcript text
// itself is already in the native script, but the label is handy when
// scanning a long history.
const LANG_NAMES = {
  en: 'English',  hi: 'Hindi',   mr: 'Marathi', bn: 'Bengali',
  gu: 'Gujarati', pa: 'Punjabi', ta: 'Tamil',   te: 'Telugu',
  kn: 'Kannada',  ml: 'Malayalam',
};

// agentDisplayName scans an AI turn's text for the persona name the AI
// announced ("Hi …, I'm Aditya …" / "this is Raj …" / "मैं कबीर बात कर रहा
// हूं …"). Returns the bare name when found, otherwise the generic "AI"
// fallback. This avoids the old hardcoded "Arjun (AI)" label, which was
// wrong whenever the campaign used a non-default voice (Aditya, Raj, Meera,
// Kabir, …) — the bubble label now matches what the AI actually said.
//
// We compute this once per transcript (not per turn) so every AI bubble
// inside the same call shows the same name. If the first AI turn is too
// short or doesn't include a self-introduction, later turns are scanned as
// a fallback before giving up and returning "AI".
const NAME_PATTERNS = [
  /\bI[' ]?m\s+([A-Z][a-zA-Z]{1,18})/,           // "I'm Aditya", "I am Raj"
  /\bI am\s+([A-Z][a-zA-Z]{1,18})/,
  /\bthis is\s+([A-Z][a-zA-Z]{1,18})/i,           // "This is Aditya"
  /(?:मैं|मे)\s+([A-Z][a-zA-Z]{1,18})/,           // Devanagari sentence with Roman name
  /(?:मैं|मे)\s+([ऀ-ॿ]{2,12})\s+(?:बात|बोल)/, // "मैं कबीर बात कर रहा हूँ"
];
function extractAgentName(turns) {
  if (!Array.isArray(turns)) return 'AI';
  for (const t of turns) {
    if ((t.role || '').toLowerCase() !== 'ai' && (t.role || '').toLowerCase() !== 'model') continue;
    const text = (t.text || t.Text || '').slice(0, 240); // cap regex work
    if (!text) continue;
    for (const re of NAME_PATTERNS) {
      const m = text.match(re);
      if (m && m[1]) return m[1].trim();
    }
  }
  return 'AI';
}

export default function TranscriptModal({ transcriptLead, setTranscriptLead, transcripts, orgTimezone }) {
  const list = Array.isArray(transcripts) ? transcripts : [];
  // Esc-to-close so there's always a keyboard escape even if the ✕ is
  // somehow hidden by an overflow/scroll edge case.
  useEffect(() => {
    if (!transcriptLead) return;
    const onKey = (e) => { if (e.key === 'Escape') setTranscriptLead(null); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [transcriptLead, setTranscriptLead]);

  if (!transcriptLead) return null;

  const close = () => setTranscriptLead(null);

  return (
    // Clicking the dark backdrop closes — instinctive escape.
    <div className="modal-overlay" onClick={e => { if (e.target === e.currentTarget) close(); }}>
      <div className="modal-content glass-panel" style={{position: 'relative', background: 'rgba(15, 23, 42, 0.97)', border: '1px solid rgba(99, 102, 241, 0.2)', maxWidth: '700px', maxHeight: '85vh', display: 'flex', flexDirection: 'column'}}>

        <div style={{flexShrink: 0, display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', borderBottom: '1px solid rgba(255,255,255,0.05)', paddingBottom: '1rem'}}>
          <div>
            <h2 style={{marginTop: 0, marginBottom: '4px', color: '#818cf8', display: 'flex', alignItems: 'center', gap: '8px'}}>📋 Call Transcripts</h2>
            <p style={{margin: 0, color: '#94a3b8', fontSize: '0.9rem'}}>{transcriptLead.first_name} — {transcriptLead.phone}</p>
          </div>
        </div>

        <div style={{flex: 1, minHeight: 0, overflowY: 'auto', paddingRight: '8px'}}>
          {list.length === 0 ? (
            <div style={{padding: '3rem', textAlign: 'center', color: '#64748b', background: 'rgba(0,0,0,0.2)', borderRadius: '12px'}}>
              <div style={{fontSize: '2rem', marginBottom: '12px'}}>📞</div>
              <div>No call transcripts yet.</div>
              <div style={{fontSize: '0.85rem', marginTop: '8px'}}>Transcripts will appear here after AI calls are completed.</div>
            </div>
          ) : (
            list.map((t, idx) => {
            // Resolve the agent name once per call so every AI bubble
            // inside the same transcript shows the same persona.
            const agentName = extractAgentName(t.transcript);
            return (
              <div key={t.id || idx} style={{marginBottom: '1.5rem', background: 'rgba(0,0,0,0.2)', borderRadius: '12px', padding: '1.25rem', border: '1px solid rgba(255,255,255,0.05)'}}>
                <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                  <div style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
                    <span style={{color: '#818cf8', fontWeight: 600}}>Call #{list.length - idx}</span>
                    <span style={{fontSize: '0.8rem', color: '#64748b'}}>{formatDateTime(t.created_at, orgTimezone)}</span>
                  </div>
                  <div style={{display: 'flex', gap: '6px', alignItems: 'center'}}>
                    {t.tts_language && (
                      <span
                        className="badge"
                        title={`Call language: ${LANG_NAMES[t.tts_language] || t.tts_language}`}
                        style={{background: 'rgba(34, 197, 94, 0.1)', color: '#4ade80', fontSize: '0.75rem', border: '1px solid rgba(34, 197, 94, 0.25)'}}
                      >🗣 {LANG_NAMES[t.tts_language] || t.tts_language.toUpperCase()}</span>
                    )}
                    {t.call_duration_s > 0 && (
                      <span className="badge" style={{background: 'rgba(99, 102, 241, 0.1)', color: '#818cf8', fontSize: '0.75rem'}}>{Math.round(t.call_duration_s)}s</span>
                    )}
                  </div>
                </div>

                {/* Audio Player — color-coded by source */}
                {t.recording_url && (() => {
                  const url = t.recording_url || '';
                  const isWav = url.endsWith('.wav');
                  const isMp3 = url.endsWith('.mp3');
                  const isWebm = url.endsWith('.webm');
                  const sourceLabel = isWav ? '🖥️ Server Recording (Stereo)' : isMp3 ? '📞 Exotel Recording' : isWebm ? '🌐 Browser Recording' : '🔊 Recording';
                  const color = isWav ? '#22d3ee' : isMp3 ? '#22c55e' : isWebm ? '#a855f7' : '#818cf8';
                  const bg = isWav ? 'rgba(34,211,238,0.05)' : isMp3 ? 'rgba(34,197,94,0.05)' : isWebm ? 'rgba(168,85,247,0.05)' : 'rgba(99,102,241,0.05)';
                  const border = isWav ? 'rgba(34,211,238,0.2)' : isMp3 ? 'rgba(34,197,94,0.2)' : isWebm ? 'rgba(168,85,247,0.2)' : 'rgba(99,102,241,0.15)';
                  // AuthAudio fetches /api/recordings/* as a blob via
                  // Authorization header to keep the JWT out of the URL.
                  return (
                    <div style={{marginBottom: '1rem', padding: '10px', background: bg, borderRadius: '8px', border: `1px solid ${border}`}}>
                      <div style={{fontSize: '0.8rem', color, marginBottom: '6px', fontWeight: 600}}>{sourceLabel}</div>
                      <AuthAudio style={{width: '100%', height: '36px'}} src={url} />
                    </div>
                  );
                })()}

                {/* Turn-by-turn transcript */}
                <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                  {(Array.isArray(t.transcript) ? t.transcript : []).map((turn, i) => (
                    <div key={i} style={{
                      display: 'flex',
                      flexDirection: turn.role === 'AI' ? 'row' : 'row-reverse',
                      gap: '8px',
                      alignItems: 'flex-start'
                    }}>
                      <div style={{
                        width: '28px', height: '28px', borderRadius: '50%', flexShrink: 0,
                        display: 'flex', alignItems: 'center', justifyContent: 'center',
                        fontSize: '0.75rem', fontWeight: 700,
                        background: turn.role === 'AI' ? 'rgba(99, 102, 241, 0.2)' : 'rgba(34, 197, 94, 0.2)',
                        color: turn.role === 'AI' ? '#818cf8' : '#4ade80',
                        border: `1px solid ${turn.role === 'AI' ? 'rgba(99, 102, 241, 0.3)' : 'rgba(34, 197, 94, 0.3)'}`
                      }}>
                        {turn.role === 'AI' ? '🤖' : '👤'}
                      </div>
                      <div style={{
                        maxWidth: '75%', padding: '10px 14px', borderRadius: '12px',
                        background: turn.role === 'AI' ? 'rgba(99, 102, 241, 0.08)' : 'rgba(34, 197, 94, 0.08)',
                        border: `1px solid ${turn.role === 'AI' ? 'rgba(99, 102, 241, 0.15)' : 'rgba(34, 197, 94, 0.15)'}`,
                        color: '#e2e8f0', fontSize: '0.9rem', lineHeight: '1.5'
                      }}>
                        <div style={{fontSize: '0.7rem', fontWeight: 600, marginBottom: '4px', color: turn.role === 'AI' ? '#818cf8' : '#4ade80'}}>
                          {turn.role === 'AI' ? `${agentName} (AI)` : transcriptLead.first_name || 'User'}
                        </div>
                        {turn.text}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
            })
          )}
        </div>

        <div style={{flexShrink: 0, borderTop: '1px solid rgba(255,255,255,0.05)', paddingTop: '12px', marginTop: '12px', textAlign: 'center'}}>
          <button
            type="button"
            onClick={close}
            style={{
              background: 'rgba(99, 102, 241, 0.15)',
              border: '1px solid rgba(99, 102, 241, 0.3)',
              color: '#cbd5e1',
              padding: '8px 24px',
              borderRadius: '8px',
              fontSize: '0.9rem',
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >Close</button>
        </div>
      </div>
    </div>
  );
}
