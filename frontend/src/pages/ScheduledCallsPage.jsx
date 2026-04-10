import React, { useState, useEffect } from 'react';

export default function ScheduledCallsPage({ apiFetch, API_URL }) {
  const [scheduledCalls, setScheduledCalls] = useState([]);
  const [loading, setLoading] = useState(true);

  const fetchScheduledCalls = async () => {
    setLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/scheduled-calls`);
      setScheduledCalls(await res.json());
    } catch (e) { console.error('Failed to fetch scheduled calls', e); }
    setLoading(false);
  };

  useEffect(() => { fetchScheduledCalls(); }, []);

  const handleCancel = async (id) => {
    if (!window.confirm('Cancel this scheduled call?')) return;
    try {
      await apiFetch(`${API_URL}/scheduled-calls/${id}`, { method: 'DELETE' });
      fetchScheduledCalls();
    } catch (e) { alert('Failed to cancel'); }
  };

  const statusStyle = (status) => {
    const map = {
      pending:   { color: '#f59e0b', bg: 'rgba(245,158,11,0.1)', border: 'rgba(245,158,11,0.3)' },
      dialing:   { color: '#60a5fa', bg: 'rgba(96,165,250,0.1)', border: 'rgba(96,165,250,0.3)' },
      completed: { color: '#22c55e', bg: 'rgba(34,197,94,0.1)',  border: 'rgba(34,197,94,0.3)' },
      failed:    { color: '#ef4444', bg: 'rgba(239,68,68,0.1)',  border: 'rgba(239,68,68,0.3)' },
      cancelled: { color: '#94a3b8', bg: 'rgba(148,163,184,0.1)', border: 'rgba(148,163,184,0.3)' },
    };
    return map[status] || map.pending;
  };

  if (loading) {
    return (
      <div className="page-container">
        <div className="glass-panel" style={{padding: '2rem', textAlign: 'center', color: '#94a3b8'}}>
          Loading scheduled calls...
        </div>
      </div>
    );
  }

  return (
    <div className="page-container">
      <h2 style={{marginBottom: '1.5rem', color: '#e2e8f0'}}>Scheduled Calls</h2>

      {scheduledCalls.length === 0 ? (
        <div className="glass-panel" style={{padding: '2rem', textAlign: 'center', color: '#64748b'}}>
          No scheduled calls. Schedule calls from the CRM or campaign pages.
        </div>
      ) : (
        <div className="glass-panel" style={{overflowX: 'auto'}}>
          <table className="leads-table" style={{width: '100%'}}>
            <thead>
              <tr>
                <th>Scheduled Time</th>
                <th>Lead Name</th>
                <th>Phone</th>
                <th>Status</th>
                <th>Action</th>
              </tr>
            </thead>
            <tbody>
              {scheduledCalls.map(call => {
                const sc = statusStyle(call.status);
                return (
                  <tr key={call.id}>
                    <td style={{fontSize: '0.85rem', color: '#e2e8f0'}}>
                      {call.scheduled_time ? new Date(call.scheduled_time).toLocaleString() : '-'}
                    </td>
                    <td style={{fontWeight: 600}}>{call.lead_name || call.first_name || '-'}</td>
                    <td style={{fontFamily: 'SFMono-Regular, Consolas, monospace', color: '#cbd5e1', fontSize: '0.85rem'}}>
                      {call.phone}
                    </td>
                    <td>
                      <span style={{
                        padding: '3px 10px', borderRadius: '12px', fontSize: '0.75rem', fontWeight: 600,
                        color: sc.color, background: sc.bg, border: `1px solid ${sc.border}`,
                      }}>
                        {call.status}
                      </span>
                    </td>
                    <td>
                      {(call.status === 'pending') && (
                        <button onClick={() => handleCancel(call.id)}
                          style={{
                            background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                            color: '#fca5a5', borderRadius: '6px', padding: '4px 12px',
                            cursor: 'pointer', fontSize: '0.75rem', fontWeight: 600,
                          }}>
                          Cancel
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
