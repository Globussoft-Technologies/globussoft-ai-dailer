import React from 'react';

export default function OpsTab({
  reports, tasks, handleCompleteTask
}) {
  return (
    <div className="ops-container" style={{padding: '1rem'}}>
      {reports && (
        <div className="metrics-grid" style={{marginBottom: '3rem'}}>
          <div className="glass-panel metric-card">
            <div className="metric-label">Closed Deals</div>
            <div className="metric-value" style={{color: '#34d399'}}>{reports.closed_deals}</div>
          </div>
          <div className="glass-panel metric-card">
            <div className="metric-label">Verified Punches</div>
            <div className="metric-value">{reports.valid_site_punches}</div>
          </div>
          <div className="glass-panel metric-card">
            <div className="metric-label">Pending Tasks</div>
            <div className="metric-value" style={{color: '#fbbf24'}}>{reports.pending_internal_tasks}</div>
          </div>
        </div>
      )}

      <div className="glass-panel">
        <h2 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.25rem', fontWeight: 600}}>Internal Cross-Department Tasks</h2>
        <div className="task-list">
          {tasks.length === 0 ? (
            <p style={{color: '#94a3b8', textAlign: 'center'}}>No internal workflows active. Try closing a lead in CRM!</p>
          ) : tasks.map(t => (
            <div key={t.id} style={{
              display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              background: 'rgba(255,255,255,0.03)', padding: '16px', borderRadius: '8px', marginBottom: '12px',
              borderLeft: t.status === 'Complete' ? '4px solid #34d399' : '4px solid #fbbf24'
            }}>
              <div>
                <div style={{display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px'}}>
                  <span className="badge" style={{background: 'rgba(255,255,255,0.1)', color: '#fff', border: 'none'}}>{t.department}</span>
                  <span style={{fontSize: '0.9rem', color: '#cbd5e1'}}>Client: {t.first_name} {t.last_name}</span>
                </div>
                <p style={{margin: 0, color: t.status === 'Complete' ? '#94a3b8' : '#f8fafc', textDecoration: t.status === 'Complete' ? 'line-through' : 'none'}}>
                  {t.description}
                </p>
              </div>
              <div>
                {t.status === 'Complete' ? (
                  <span style={{color: '#34d399', fontWeight: 600, fontSize: '0.9rem'}}>✓ Done</span>
                ) : (
                  <button className="btn-call" onClick={() => handleCompleteTask(t.id)}>Mark Done</button>
                )}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
