import React, { useState, useEffect, useRef, useMemo } from 'react';
import { useAuth } from '../../contexts/AuthContext';

const PER_PAGE = 50;
const ACTIVITY_BUFFER = 1000;

function withDate(label, tsMs) {
  const d = new Date(tsMs);
  const dd = String(d.getDate()).padStart(2, '0');
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const yyyy = d.getFullYear();
  const dateStr = `${dd}/${mm}/${yyyy}`;
  if (/\[\d{2}:\d{2}:\d{2}\]/.test(label)) {
    return label.replace(/\[(\d{2}:\d{2}:\d{2})\]/, `[${dateStr} $1]`);
  }
  return `[${dateStr}] ${label}`;
}

// Each entry in activityLogs is { arrivedAt: number, line: string } so the
// date filter has a real timestamp even when the backend is still sending
// plain-text events. parseActivity extracts the rest from JSON or falls back
// to regex on legacy strings.
function parseActivity(entry) {
  const line = entry.line;
  let parsed = null;
  try {
    const j = JSON.parse(line);
    if (j && typeof j === 'object' && j.label) {
      parsed = {
        tsMs: j.ts ? new Date(j.ts).getTime() : entry.arrivedAt,
        campaignId: j.campaign_id ?? null,
        status: (j.status || '').toUpperCase(),
        leadName: j.lead_name || '',
        phone: j.phone || '',
        label: j.label,
      };
    }
  } catch (_) { /* legacy plain-text */ }
  if (!parsed) {
    const status = (line.match(/—\s*([A-Z][A-Z_-]+)/) || [])[1] || '';
    parsed = {
      tsMs: entry.arrivedAt,
      campaignId: null,
      status,
      leadName: '',
      phone: '',
      label: line,
    };
  }
  parsed.raw = parsed.label;
  return parsed;
}

export default function LogsTab({ API_URL, authToken, apiFetch }) {
  const { fetchSseTicket } = useAuth();
  const [mode, setMode] = useState('activity');
  const [filter, setFilter] = useState('');
  const [paused, setPaused] = useState(false);
  const [activityLogs, setActivityLogs] = useState([]);
  const [statusFilter, setStatusFilter] = useState('');
  const [campaignFilter, setCampaignFilter] = useState('');
  const [dateFrom, setDateFrom] = useState('');
  const [dateTo, setDateTo] = useState('');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [campaigns, setCampaigns] = useState([]);
  const verboseRef = useRef(null);
  const activityEsRef = useRef(null);
  const verboseEsRef = useRef(null);

  useEffect(() => {
    if (!apiFetch) return;
    let cancelled = false;
    apiFetch(`${API_URL}/campaigns`)
      .then(r => r.ok ? r.json() : [])
      .then(list => { if (!cancelled && Array.isArray(list)) setCampaigns(list); })
      .catch(() => { /* dropdown falls back to ids */ });
    return () => { cancelled = true; };
  }, [apiFetch, API_URL]);

  // Subscribe to per-campaign Redis channel when a campaign is selected;
  // fall back to firehose ("all") when no filter. This pushes the campaign
  // filter down to the source so legacy plain-text events also get filtered
  // correctly without needing campaign_id in the payload.
  useEffect(() => {
    if (activityEsRef.current) activityEsRef.current.close();
    const cid = campaignFilter || '0';
    setActivityLogs([]); // discard previous campaign's buffer on switch
    let cancelled = false;
    let es = null;
    fetchSseTicket().then(ticket => {
      if (cancelled) return;
      es = new EventSource(`${API_URL}/campaign-events?ticket=${encodeURIComponent(ticket)}&campaign_id=${cid}`);
      es.onmessage = (ev) => {
        if (!paused) {
          setActivityLogs(prev => [...prev.slice(-ACTIVITY_BUFFER), { arrivedAt: Date.now(), line: ev.data }]);
        }
      };
      activityEsRef.current = es;
    }).catch(() => { /* surface via console only — UI shows empty stream */ });
    return () => { cancelled = true; if (es) es.close(); };
  }, [paused, campaignFilter, API_URL, fetchSseTicket]);

  useEffect(() => {
    if (mode !== 'verbose' || !verboseRef.current) return;
    if (verboseEsRef.current) verboseEsRef.current.close();
    const el = verboseRef.current;
    el.innerHTML = '';
    let cancelled = false;
    let es = null;
    fetchSseTicket().then(ticket => {
      if (cancelled || !verboseRef.current) return;
      es = new EventSource(`${API_URL}/live-logs?ticket=${encodeURIComponent(ticket)}`);
      verboseEsRef.current = es;
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
    }).catch(() => { /* UI shows empty stream */ });
    return () => { cancelled = true; if (es) es.close(); };
  }, [mode, paused, filter, API_URL, fetchSseTicket]);

  const activityIcon = (text) => {
    if (text.includes('📞')) return { bg: 'rgba(96,165,250,0.1)', border: 'rgba(96,165,250,0.2)' };
    if (text.includes('✅') || text.includes('🎯')) return { bg: 'rgba(34,197,94,0.1)', border: 'rgba(34,197,94,0.2)' };
    if (text.includes('❌')) return { bg: 'rgba(245,158,11,0.1)', border: 'rgba(245,158,11,0.2)' };
    if (text.includes('📵') || text.includes('⚠️') || text.includes('💥')) return { bg: 'rgba(239,68,68,0.1)', border: 'rgba(239,68,68,0.2)' };
    if (text.includes('🚀') || text.includes('🏁')) return { bg: 'rgba(139,92,246,0.1)', border: 'rgba(139,92,246,0.2)' };
    return { bg: 'rgba(255,255,255,0.03)', border: 'rgba(255,255,255,0.05)' };
  };

  const parsedLogs = useMemo(() => activityLogs.map(parseActivity), [activityLogs]);

  const statusOptions = useMemo(() => {
    const s = new Set();
    parsedLogs.forEach(p => { if (p.status) s.add(p.status); });
    return Array.from(s).sort();
  }, [parsedLogs]);

  const campaignOptions = useMemo(() => {
    const seen = new Map(); // id -> display name
    campaigns.forEach(c => { if (c && c.id != null) seen.set(String(c.id), c.name || `Campaign #${c.id}`); });
    parsedLogs.forEach(p => {
      if (p.campaignId != null) {
        const k = String(p.campaignId);
        if (!seen.has(k)) seen.set(k, `Campaign #${k}`);
      }
    });
    return Array.from(seen.entries())
      .sort((a, b) => Number(a[0]) - Number(b[0]))
      .map(([id, name]) => ({ id, name }));
  }, [campaigns, parsedLogs]);

  const filteredLogs = useMemo(() => {
    const q = search.trim().toLowerCase();
    const fromMs = dateFrom ? new Date(dateFrom + 'T00:00:00').getTime() : null;
    const toMs = dateTo ? new Date(dateTo + 'T23:59:59.999').getTime() : null;
    return parsedLogs.filter(p => {
      if (statusFilter && p.status !== statusFilter) return false;
      // campaign filter is enforced at SSE subscription, not here
      if (fromMs !== null && p.tsMs < fromMs) return false;
      if (toMs !== null && p.tsMs > toMs) return false;
      if (q && !(p.raw + ' ' + p.leadName + ' ' + p.phone).toLowerCase().includes(q)) return false;
      return true;
    });
  }, [parsedLogs, statusFilter, dateFrom, dateTo, search]);

  const reversedLogs = useMemo(() => [...filteredLogs].reverse(), [filteredLogs]);

  const totalPages = Math.max(1, Math.ceil(reversedLogs.length / PER_PAGE));
  const safePage = Math.min(page, totalPages);
  const pageLogs = reversedLogs.slice((safePage - 1) * PER_PAGE, safePage * PER_PAGE);

  useEffect(() => { setPage(1); }, [statusFilter, campaignFilter, dateFrom, dateTo, search]);

  const handleClear = () => {
    if (!window.confirm('Clear all visible logs from this view? Streamed events will continue to arrive after clearing.')) return;
    setActivityLogs([]);
    if (verboseRef.current) verboseRef.current.innerHTML = '';
  };

  return (
    <div style={{padding: '1rem'}}>
      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '10px'}}>
        <h2 style={{margin: 0}}>📡 System Logs</h2>
        <div style={{display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap'}}>
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

          {mode === 'verbose' && (
            <input className="form-input" placeholder="Filter logs..." value={filter}
              onChange={e => setFilter(e.target.value)}
              style={{width: '160px', height: '30px', fontSize: '0.8rem', padding: '4px 8px'}} />
          )}

          <button onClick={() => setPaused(!paused)}
            style={{padding: '6px 12px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)',
              background: paused ? 'rgba(239,68,68,0.15)' : 'rgba(34,197,94,0.15)',
              color: paused ? '#ef4444' : '#22c55e', cursor: 'pointer', fontSize: '0.8rem', fontWeight: 600}}>
            {paused ? '⏸ Paused' : '▶ Live'}
          </button>

          <button onClick={handleClear}
            style={{padding: '6px 12px', borderRadius: '6px', border: '1px solid rgba(239,68,68,0.2)',
              background: 'rgba(239,68,68,0.1)', color: '#fca5a5', cursor: 'pointer', fontSize: '0.8rem'}}>
            🗑️ Clear
          </button>
        </div>
      </div>

      {mode === 'activity' && (
        <div style={{display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '10px', flexWrap: 'wrap'}}>
          <select className="form-input" value={statusFilter} onChange={e => setStatusFilter(e.target.value)}
            style={{height: '30px', fontSize: '0.8rem', padding: '2px 8px', width: '150px'}}>
            <option value="">All statuses</option>
            {statusOptions.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
          <select className="form-input" value={campaignFilter} onChange={e => setCampaignFilter(e.target.value)}
            style={{height: '30px', fontSize: '0.8rem', padding: '2px 8px', width: '180px'}}>
            <option value="">All campaigns</option>
            {campaignOptions.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
          <input type="date" className="form-input" value={dateFrom} onChange={e => setDateFrom(e.target.value)}
            title="From date"
            style={{height: '30px', fontSize: '0.8rem', padding: '2px 8px', width: '140px'}} />
          <input type="date" className="form-input" value={dateTo} onChange={e => setDateTo(e.target.value)}
            title="To date"
            style={{height: '30px', fontSize: '0.8rem', padding: '2px 8px', width: '140px'}} />
          <input className="form-input" placeholder="Search name or phone..." value={search}
            onChange={e => setSearch(e.target.value)}
            style={{height: '30px', fontSize: '0.8rem', padding: '2px 8px', width: '200px'}} />
          {(statusFilter || campaignFilter || dateFrom || dateTo || search) && (
            <button onClick={() => { setStatusFilter(''); setCampaignFilter(''); setDateFrom(''); setDateTo(''); setSearch(''); }}
              style={{padding: '4px 10px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)',
                background: 'rgba(255,255,255,0.05)', color: '#cbd5e1', cursor: 'pointer', fontSize: '0.75rem'}}>
              Reset
            </button>
          )}
          <span style={{fontSize: '0.75rem', color: '#64748b'}}>
            {filteredLogs.length} of {activityLogs.length} events
          </span>
        </div>
      )}

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

      {mode === 'activity' && (
        <>
          <div style={{background: 'rgba(2,6,23,0.8)', border: '1px solid rgba(255,255,255,0.06)', borderRadius: '8px', height: '64vh', overflowY: 'auto', padding: '8px'}}>
            {pageLogs.length === 0 ? (
              <div style={{textAlign: 'center', color: '#64748b', padding: '3rem'}}>
                <div style={{fontSize: '2rem', marginBottom: '12px'}}>📡</div>
                <div>{activityLogs.length === 0 ? 'Waiting for campaign activity...' : 'No events match the current filters.'}</div>
                {activityLogs.length === 0 && (
                  <div style={{fontSize: '0.8rem', marginTop: '8px'}}>Start a campaign dial to see live events here.</div>
                )}
              </div>
            ) : (
              pageLogs.map((p, i) => {
                const style = activityIcon(p.raw);
                return (
                  <div key={`${safePage}-${i}`} style={{
                    padding: '8px 12px', marginBottom: '4px', borderRadius: '6px',
                    background: style.bg, borderLeft: `3px solid ${style.border}`,
                    fontSize: '0.85rem', color: '#e2e8f0', fontFamily: 'system-ui'
                  }}>
                    {withDate(p.raw, p.tsMs)}
                  </div>
                );
              })
            )}
          </div>

          <div style={{display: 'flex', justifyContent: 'center', alignItems: 'center', gap: '12px', marginTop: '10px', fontSize: '0.8rem'}}>
            <button onClick={() => setPage(p => Math.max(1, p - 1))} disabled={safePage <= 1}
              style={{padding: '4px 12px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)',
                background: 'rgba(255,255,255,0.05)', color: safePage <= 1 ? '#475569' : '#cbd5e1',
                cursor: safePage <= 1 ? 'not-allowed' : 'pointer'}}>
              ← Prev
            </button>
            <span style={{color: '#94a3b8'}}>Page {safePage} of {totalPages}</span>
            <button onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={safePage >= totalPages}
              style={{padding: '4px 12px', borderRadius: '6px', border: '1px solid rgba(255,255,255,0.1)',
                background: 'rgba(255,255,255,0.05)', color: safePage >= totalPages ? '#475569' : '#cbd5e1',
                cursor: safePage >= totalPages ? 'not-allowed' : 'pointer'}}>
              Next →
            </button>
          </div>
        </>
      )}

      {mode === 'verbose' && (
        <div ref={verboseRef} style={{
          background: 'rgba(2,6,23,0.8)', border: '1px solid rgba(255,255,255,0.06)',
          borderRadius: '8px', height: '70vh', overflowY: 'auto', overflowX: 'hidden'
        }} />
      )}
    </div>
  );
}
