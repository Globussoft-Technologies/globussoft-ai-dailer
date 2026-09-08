import React, { useMemo, useState } from 'react';
import { useToast } from '../contexts/UIContext';
import { useAgentReport } from '../hooks/useQueries';
import UserDetailModal from '../components/modals/UserDetailModal';

const T = {
  bg: '#f4f5f9', card: '#ffffff', border: '#e5e7eb',
  accent: '#6366f1', green: '#10b981', amber: '#f59e0b', red: '#ef4444',
  text: '#111827', sub: '#374151', muted: '#9ca3af',
  font: "'DM Sans', sans-serif", mono: "'DM Mono', monospace",
};

const card = {
  background: T.card,
  border: `1px solid ${T.border}`,
  borderRadius: 12,
  boxShadow: '0 1px 3px rgba(0,0,0,0.06), 0 4px 12px rgba(0,0,0,0.04)',
};

const inputStyle = {
  padding: '10px 12px',
  borderRadius: 8,
  fontSize: 13,
  border: `1px solid ${T.border}`,
  background: '#fff',
  color: T.text,
  fontFamily: T.font,
  outline: 'none',
  width: '100%',
  boxSizing: 'border-box',
};

const btnPrimary = {
  background: T.accent,
  color: '#fff',
  border: 'none',
  borderRadius: 8,
  padding: '10px 18px',
  fontWeight: 700,
  cursor: 'pointer',
  fontFamily: T.font,
  fontSize: 13,
};

const btnGhost = {
  background: '#fff',
  color: T.sub,
  border: `1px solid ${T.border}`,
  borderRadius: 8,
  padding: '10px 14px',
  fontWeight: 700,
  cursor: 'pointer',
  fontFamily: T.font,
  fontSize: 13,
};

const thStyle = {
  textAlign: 'left',
  padding: '11px 12px',
  color: T.muted,
  fontSize: 10,
  fontWeight: 800,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  borderBottom: `1px solid ${T.border}`,
  background: '#fbfcff',
  whiteSpace: 'nowrap',
};

const tdStyle = {
  padding: '13px 12px',
  color: T.sub,
  fontSize: 13,
  borderBottom: `1px solid ${T.border}`,
  verticalAlign: 'middle',
  whiteSpace: 'nowrap',
};

const thStickyStyle = {
  ...thStyle,
  position: 'sticky',
  top: 0,
  zIndex: 1,
};

function Metric({ value, positive, negative }) {
  const n = value || 0;
  if (n === 0) {
    return <span style={{ color: T.muted }}>-</span>;
  }
  let color = T.sub;
  if (positive) color = T.green;
  if (negative) color = T.red;
  return <span style={{ color, fontWeight: 800 }}>{n}</span>;
}

function todayStr() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function monthStr() {
  return todayStr().slice(0, 7);
}

function lastDayOfMonth(yyyyMm) {
  const [y, m] = yyyyMm.split('-').map(Number);
  return new Date(y, m, 0).getDate();
}

function Badge({ children, color = T.accent }) {
  return (
    <span style={{
      display: 'inline-flex',
      alignItems: 'center',
      padding: '3px 9px',
      borderRadius: 999,
      fontSize: 10,
      fontWeight: 800,
      color,
      background: `${color}18`,
      border: `1px solid ${color}35`,
    }}>
      {children}
    </span>
  );
}

export default function AgentReportPage({ apiFetch, API_URL, campaigns = [] }) {
  const toast = useToast();
  const [downloading, setDownloading] = useState(false);
  const [campaignId, setCampaignId] = useState('');
  const [period, setPeriod] = useState('daily');
  const [day, setDay] = useState(todayStr());
  const [month, setMonth] = useState(monthStr());
  const [from, setFrom] = useState(todayStr());
  const [to, setTo] = useState(todayStr());
  const [roleFilter, setRoleFilter] = useState('All');
  const [searchTerm, setSearchTerm] = useState('');
  const [modalUserId, setModalUserId] = useState(null);

  const maxDate = todayStr();

  const displayRows = useMemo(() => {
    let filtered = rows;
    if (roleFilter !== 'All') {
      filtered = filtered.filter((r) => r.role === roleFilter);
    }
    if (searchTerm.trim()) {
      const q = searchTerm.toLowerCase();
      filtered = filtered.filter((r) =>
        (r.full_name || '').toLowerCase().includes(q) ||
        (r.email || '').toLowerCase().includes(q)
      );
    }
    return filtered;
  }, [rows, roleFilter, searchTerm]);

  const range = useMemo(() => {
    if (period === 'monthly') {
      return { from: `${month}-01`, to: `${month}-${String(lastDayOfMonth(month)).padStart(2, '0')}` };
    }
    if (period === 'custom') return { from, to };
    return { from: day, to: day };
  }, [period, day, month, from, to]);

  const { data: rows = [], isLoading: loading } = useAgentReport({
    from: range.from,
    to: range.to,
    campaignId: campaignId ? Number(campaignId) : undefined,
  });

  const handleDownload = async () => {
    setDownloading(true);
    try {
      const params = new URLSearchParams({ from: range.from, to: range.to });
      if (campaignId) params.set('campaign_id', campaignId);
      const res = await apiFetch(`${API_URL}/analytics/agent-report?${params.toString()}`);
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || `HTTP ${res.status}`);
      }
      const blob = await res.blob();
      const objectUrl = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = objectUrl;
      a.download = `agent_report_${range.from}_to_${range.to}.xlsx`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(objectUrl);
      toast('Agent report downloaded');
    } catch (e) {
      toast(`Download failed: ${e.message}`, 'error');
    }
    setDownloading(false);
  };

  return (
    <div style={{ padding: '28px 32px', background: T.bg, minHeight: '100%', fontFamily: T.font }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16, marginBottom: 22 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 800, color: T.text }}>Agent Performance Report</h1>
          <p style={{ margin: '6px 0 0', color: T.muted, fontSize: 14 }}>
            Review calls, recordings, outcomes, and appointments by agent and campaign.
          </p>
        </div>
        <button onClick={handleDownload} disabled={downloading} style={{ ...btnPrimary, opacity: downloading ? 0.65 : 1 }}>
          {downloading ? 'Downloading...' : 'Download Excel'}
        </button>
      </div>

      <div style={{ ...card, padding: 18, marginBottom: 18 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 12, alignItems: 'end' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 150, flex: '1 1 140px' }}>
            <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Campaign</span>
            <select value={campaignId} onChange={e => setCampaignId(e.target.value)} style={{ ...inputStyle, height: 41 }}>
              <option value="">All campaigns</option>
              {campaigns.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 140, flex: '1 1 120px' }}>
            <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Period</span>
            <select value={period} onChange={e => setPeriod(e.target.value)} style={{ ...inputStyle, height: 41 }}>
              <option value="daily">Daily</option>
              <option value="monthly">Monthly</option>
              <option value="custom">Custom Date</option>
            </select>
          </label>
          {period === 'daily' && (
            <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 150, flex: '1 1 140px' }}>
              <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Date</span>
              <input type="date" value={day} max={maxDate} onChange={e => setDay(e.target.value)} style={inputStyle} />
            </label>
          )}
          {period === 'monthly' && (
            <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 150, flex: '1 1 140px' }}>
              <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Month</span>
              <input type="month" value={month} max={monthStr()} onChange={e => setMonth(e.target.value)} style={inputStyle} />
            </label>
          )}
          {period === 'custom' && (
            <>
              <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 150, flex: '1 1 140px' }}>
                <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>From</span>
                <input type="date" value={from} max={maxDate} onChange={e => setFrom(e.target.value)} style={inputStyle} />
              </label>
              <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 150, flex: '1 1 140px' }}>
                <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>To</span>
                <input type="date" value={to} max={maxDate} min={from} onChange={e => setTo(e.target.value)} style={inputStyle} />
              </label>
            </>
          )}
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 130, flex: '1 1 120px' }}>
            <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Role</span>
            <select value={roleFilter} onChange={e => setRoleFilter(e.target.value)} style={{ ...inputStyle, height: 41 }}>
              <option value="All">All</option>
              <option value="Agent">Agent</option>
              <option value="Executive">Executive</option>
            </select>
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 180, flex: '2 1 200px' }}>
            <span style={{ fontSize: 11, fontWeight: 800, color: T.sub, textTransform: 'uppercase' }}>Search name/email</span>
            <input
              type="text"
              value={searchTerm}
              onChange={e => setSearchTerm(e.target.value)}
              placeholder="Type to filter..."
              style={inputStyle}
            />
          </label>
          <button
            type="button"
            onClick={() => {
              setCampaignId('');
              setPeriod('daily');
              setDay(todayStr());
              setMonth(monthStr());
              setFrom(todayStr());
              setTo(todayStr());
              setRoleFilter('All');
              setSearchTerm('');
            }}
            style={btnGhost}
          >
            Reset
          </button>
        </div>
      </div>

      <div style={{ ...card, overflow: 'hidden' }}>
        <div style={{ padding: '14px 16px', borderBottom: `1px solid ${T.border}`, display: 'flex', justifyContent: 'space-between', gap: 12 }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 800, color: T.text }}>Agent performance</div>
            <div style={{ fontSize: 12, color: T.muted, marginTop: 2 }}>{range.from} to {range.to}</div>
          </div>
          {loading && <Badge color={T.amber}>Loading</Badge>}
        </div>
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', minWidth: 1050 }}>
            <thead>
              <tr>
                {['Name', 'Role', 'Calls', 'Connected', 'Completed', 'Unanswered', 'Busy', 'Failed', 'Recording', 'Appointments', 'Notes'].map(h => (
                  <th key={h} style={thStickyStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {displayRows.map((r, i) => {
                const last = i === displayRows.length - 1;
                const rowTd = { ...tdStyle, borderBottom: last ? 'none' : `1px solid ${T.border}` };
                return (
                  <tr
                    key={r.user_id}
                    onClick={() => setModalUserId(r.user_id)}
                    style={{ cursor: 'pointer' }}
                    onMouseEnter={(e) => { e.currentTarget.style.background = '#f9fafb'; }}
                    onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
                  >
                    <td style={{ ...rowTd, color: T.text, fontWeight: 800 }}>
                      <div>{r.full_name || '-'}</div>
                      <div style={{ color: T.muted, fontSize: 11, fontWeight: 500 }}>{r.email}</div>
                    </td>
                    <td style={rowTd}><Badge>{r.role}</Badge></td>
                    <td style={rowTd}><Metric value={r.total_calls} /></td>
                    <td style={rowTd}><Metric value={r.connected} positive /></td>
                    <td style={rowTd}><Metric value={r.completed} positive /></td>
                    <td style={rowTd}><Metric value={r.unanswered} /></td>
                    <td style={rowTd}><Metric value={r.busy} /></td>
                    <td style={rowTd}><Metric value={r.failed} negative /></td>
                    <td style={rowTd}><Metric value={r.recordings} /></td>
                    <td style={rowTd}><Metric value={r.appointments} positive /></td>
                    <td style={rowTd}><Metric value={r.notes_added} /></td>
                  </tr>
                );
              })}
              {!loading && displayRows.length > 0 && (
                <tr style={{ background: '#fbfcff', fontWeight: 800 }}>
                  <td style={{ ...tdStyle, color: T.text, borderTop: `2px solid ${T.border}` }}>Total</td>
                  <td style={{ ...tdStyle, borderTop: `2px solid ${T.border}` }}>-</td>
                  {['total_calls', 'connected', 'completed', 'unanswered', 'busy', 'failed', 'recordings', 'appointments', 'notes_added'].map(k => (
                    <td key={k} style={{ ...tdStyle, borderTop: `2px solid ${T.border}` }}>
                      <Metric value={displayRows.reduce((s, r) => s + (Number(r[k]) || 0), 0)} positive={k !== 'failed' && k !== 'unanswered'} negative={k === 'failed'} />
                    </td>
                  ))}
                </tr>
              )}
              {!loading && displayRows.length === 0 && (
                <tr>
                  <td colSpan="11" style={{ ...tdStyle, textAlign: 'center', color: T.muted, padding: 30 }}>
                    No agent performance found for this filter.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {modalUserId && (
        <UserDetailModal
          userId={modalUserId}
          onClose={() => setModalUserId(null)}
          apiFetch={apiFetch}
          API_URL={API_URL}
          range={range}
        />
      )}
    </div>
  );
}
