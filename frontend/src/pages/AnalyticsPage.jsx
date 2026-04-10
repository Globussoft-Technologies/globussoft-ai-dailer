import React, { useState, useEffect } from 'react';

export default function AnalyticsPage({ apiFetch, API_URL }) {
  const [data, setData] = useState(null);
  const [langData, setLangData] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      try {
        const [dashRes, langRes] = await Promise.all([
          apiFetch(`${API_URL}/analytics/dashboard`),
          apiFetch(`${API_URL}/analytics/languages`),
        ]);
        setData(await dashRes.json());
        setLangData(await langRes.json());
      } catch (e) {
        console.error('Failed to load analytics', e);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  if (loading) return <div style={{padding: '3rem', textAlign: 'center', color: '#94a3b8'}}>Loading analytics...</div>;
  if (!data) return <div style={{padding: '3rem', textAlign: 'center', color: '#94a3b8'}}>Failed to load analytics data.</div>;

  const maxDaily = Math.max(...data.daily_calls.map(d => d.count), 1);
  const sentimentTotal = (data.sentiment_breakdown.positive + data.sentiment_breakdown.neutral + data.sentiment_breakdown.negative) || 1;

  return (
    <div className="analytics-container">
      <div className="wa-header" style={{borderBottom: '1px solid rgba(255,255,255,0.05)', marginBottom: '2rem'}}>
        <h3><span style={{color: '#f59e0b'}}>Analytics</span> Dashboard</h3>
        <p>Real-time metrics from your AI dialer campaigns.</p>
      </div>

      {/* Top Stats Row */}
      <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem', padding: '0 24px', marginBottom: '2rem'}}>
        <StatCard label="Total Calls" value={data.total_calls} />
        <StatCard label="Calls Today" value={data.calls_today} />
        <StatCard label="Pickup Rate" value={`${Math.round(data.pickup_rate * 100)}%`} color={data.pickup_rate >= 0.5 ? '#22c55e' : '#ef4444'} />
        <StatCard label="Appointment Rate" value={`${Math.round(data.appointment_rate * 100)}%`} color={data.appointment_rate >= 0.2 ? '#22c55e' : '#f59e0b'} />
        <StatCard label="Avg Duration" value={`${Math.round(data.avg_call_duration_sec)}s`} />
        <StatCard label="This Week" value={data.calls_this_week} />
      </div>

      <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '1.5rem', padding: '0 24px', marginBottom: '2rem'}}>
        {/* Daily Calls Bar Chart */}
        <div className="glass-panel" style={{padding: '1.5rem'}}>
          <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.85rem', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '1.5rem'}}>Daily Calls (Last 7 Days)</h4>
          <div style={{display: 'flex', alignItems: 'flex-end', gap: '8px', height: '160px'}}>
            {data.daily_calls.map((d, i) => {
              const pct = Math.max(4, (d.count / maxDaily) * 100);
              const dayLabel = new Date(d.date + 'T12:00:00').toLocaleDateString('en-US', { weekday: 'short' });
              return (
                <div key={i} style={{flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center', height: '100%', justifyContent: 'flex-end'}}>
                  <span style={{fontSize: '0.75rem', color: '#e2e8f0', marginBottom: '4px'}}>{d.count}</span>
                  <div style={{
                    width: '100%',
                    maxWidth: '48px',
                    height: `${pct}%`,
                    background: 'linear-gradient(180deg, #f59e0b, #d97706)',
                    borderRadius: '4px 4px 0 0',
                    transition: 'height 0.3s ease',
                  }} />
                  <span style={{fontSize: '0.7rem', color: '#64748b', marginTop: '6px'}}>{dayLabel}</span>
                </div>
              );
            })}
          </div>
        </div>

        {/* Sentiment Breakdown */}
        <div className="glass-panel" style={{padding: '1.5rem'}}>
          <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.85rem', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '1.5rem'}}>Customer Sentiment</h4>
          <div style={{display: 'flex', flexDirection: 'column', gap: '1rem'}}>
            <SentimentBar label="Positive" count={data.sentiment_breakdown.positive} total={sentimentTotal} color="#22c55e" />
            <SentimentBar label="Neutral" count={data.sentiment_breakdown.neutral} total={sentimentTotal} color="#f59e0b" />
            <SentimentBar label="Negative" count={data.sentiment_breakdown.negative} total={sentimentTotal} color="#ef4444" />
          </div>
          {sentimentTotal <= 1 && data.sentiment_breakdown.positive === 0 && (
            <p style={{color: '#64748b', fontSize: '0.85rem', marginTop: '1rem'}}>No sentiment data yet. Reviews are generated after calls complete.</p>
          )}
        </div>
      </div>

      {/* Campaign Performance Table */}
      <div style={{padding: '0 24px', marginBottom: '2rem'}}>
        <div className="glass-panel" style={{padding: '1.5rem'}}>
          <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.85rem', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '1rem'}}>Campaign Performance</h4>
          {data.campaign_performance.length === 0 ? (
            <p style={{color: '#64748b', fontSize: '0.9rem'}}>No campaigns found.</p>
          ) : (
            <div style={{overflowX: 'auto'}}>
              <table style={{width: '100%', borderCollapse: 'collapse', fontSize: '0.9rem'}}>
                <thead>
                  <tr style={{borderBottom: '1px solid rgba(255,255,255,0.1)'}}>
                    <th style={thStyle}>Campaign</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Calls</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Appointments</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Avg Score</th>
                  </tr>
                </thead>
                <tbody>
                  {data.campaign_performance.map((c) => (
                    <tr key={c.campaign_id} style={{borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                      <td style={tdStyle}>{c.name}</td>
                      <td style={{...tdStyle, textAlign: 'center'}}>{c.calls}</td>
                      <td style={{...tdStyle, textAlign: 'center'}}>
                        <span style={{color: c.appointments > 0 ? '#22c55e' : '#64748b'}}>{c.appointments}</span>
                      </td>
                      <td style={{...tdStyle, textAlign: 'center'}}>
                        <ScoreBadge score={c.avg_score} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>

      {/* Top Failure Reasons */}
      {data.top_failure_reasons.length > 0 && (
        <div style={{padding: '0 24px', marginBottom: '2rem'}}>
          <div className="glass-panel" style={{padding: '1.5rem'}}>
            <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.85rem', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '1rem'}}>Top Failure Reasons</h4>
            <div style={{display: 'flex', flexDirection: 'column', gap: '0.5rem'}}>
              {data.top_failure_reasons.map((r, i) => (
                <div key={i} style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.5rem 0', borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                  <span style={{color: '#e2e8f0', fontSize: '0.85rem'}}>{r.reason}</span>
                  <span style={{color: '#ef4444', fontWeight: 600, fontSize: '0.85rem'}}>{r.count}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Language Performance */}
      <div style={{padding: '0 24px', marginBottom: '2rem'}}>
        <div className="glass-panel" style={{padding: '1.5rem'}}>
          <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.85rem', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '1rem'}}>Language Performance</h4>
          {langData.length === 0 ? (
            <p style={{color: '#64748b', fontSize: '0.9rem'}}>No language data yet. Calls need campaign language settings to appear here.</p>
          ) : (
            <div style={{overflowX: 'auto'}}>
              <table style={{width: '100%', borderCollapse: 'collapse', fontSize: '0.9rem'}}>
                <thead>
                  <tr style={{borderBottom: '1px solid rgba(255,255,255,0.1)'}}>
                    <th style={thStyle}>Language</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Total Calls</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Appointments</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Conversion Rate</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Avg Quality</th>
                    <th style={{...thStyle, textAlign: 'center'}}>Avg Duration</th>
                  </tr>
                </thead>
                <tbody>
                  {langData.map((row) => (
                    <tr key={row.language} style={{borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                      <td style={tdStyle}>{LANG_NAMES[row.language] || row.language}</td>
                      <td style={{...tdStyle, textAlign: 'center'}}>{row.total_calls}</td>
                      <td style={{...tdStyle, textAlign: 'center'}}>
                        <span style={{color: row.appointments > 0 ? '#22c55e' : '#64748b'}}>{row.appointments}</span>
                      </td>
                      <td style={{...tdStyle, textAlign: 'center'}}>{row.conversion_rate}%</td>
                      <td style={{...tdStyle, textAlign: 'center'}}>
                        <ScoreBadge score={row.avg_score} />
                      </td>
                      <td style={{...tdStyle, textAlign: 'center'}}>{Math.round(row.avg_duration)}s</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, color }) {
  return (
    <div className="glass-panel" style={{padding: '1.25rem', textAlign: 'center'}}>
      <div style={{fontSize: '0.75rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '1px', marginBottom: '0.5rem'}}>{label}</div>
      <div style={{fontSize: '1.75rem', fontWeight: 700, color: color || '#e2e8f0'}}>{value}</div>
    </div>
  );
}

function SentimentBar({ label, count, total, color }) {
  const pct = Math.round((count / total) * 100);
  return (
    <div>
      <div style={{display: 'flex', justifyContent: 'space-between', marginBottom: '4px', fontSize: '0.85rem'}}>
        <span style={{color: '#e2e8f0'}}>{label}</span>
        <span style={{color: '#94a3b8'}}>{count} ({pct}%)</span>
      </div>
      <div style={{height: '8px', background: 'rgba(255,255,255,0.08)', borderRadius: '4px', overflow: 'hidden'}}>
        <div style={{height: '100%', width: `${pct}%`, background: color, borderRadius: '4px', transition: 'width 0.3s ease'}} />
      </div>
    </div>
  );
}

function ScoreBadge({ score }) {
  let color = '#64748b';
  if (score >= 4) color = '#22c55e';
  else if (score >= 3) color = '#f59e0b';
  else if (score > 0) color = '#ef4444';
  return <span style={{color, fontWeight: 600}}>{score > 0 ? score.toFixed(1) : '--'}</span>;
}

const LANG_NAMES = {
  hi: 'Hindi', bn: 'Bengali', mr: 'Marathi', en: 'English',
  ta: 'Tamil', te: 'Telugu', kn: 'Kannada', ml: 'Malayalam',
  gu: 'Gujarati', pa: 'Punjabi', or: 'Odia', as: 'Assamese',
};

const thStyle = { padding: '0.75rem 1rem', textAlign: 'left', color: '#94a3b8', fontWeight: 600, fontSize: '0.8rem', textTransform: 'uppercase', letterSpacing: '0.5px' };
const tdStyle = { padding: '0.75rem 1rem', color: '#e2e8f0' };
