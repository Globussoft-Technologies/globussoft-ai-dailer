import React from 'react';

export default function CrmTab({
  userRole, API_URL,
  activeVoiceProvider, setActiveVoiceProvider, activeVoiceId, setActiveVoiceId,
  activeLanguage, setActiveLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  selectedOrg, apiFetch, savedVoiceName, setSavedVoiceName,
  campaigns, onCampaignClick
}) {
  // Aggregate stats across all campaigns
  const totalLeads = campaigns.reduce((sum, c) => sum + (c.stats?.total || 0), 0);
  const totalCalled = campaigns.reduce((sum, c) => sum + (c.stats?.called || 0), 0);
  const totalQualified = campaigns.reduce((sum, c) => sum + (c.stats?.qualified || 0), 0);
  const totalAppointments = campaigns.reduce((sum, c) => sum + (c.stats?.appointments || 0), 0);
  const activeCampaigns = campaigns.filter(c => c.status === 'active');

  return (
    <div className="crm-container">
      <h2 style={{marginTop: 0, marginBottom: '1.5rem'}}>Dashboard</h2>

      {/* Aggregate Metrics */}
      <div className="metrics-grid" style={{marginBottom: '2rem'}}>
        <div className="glass-panel metric-card">
          <div className="metric-label">Campaigns</div>
          <div className="metric-value">{activeCampaigns.length}</div>
        </div>
        <div className="glass-panel metric-card">
          <div className="metric-label">Total Leads</div>
          <div className="metric-value">{totalLeads}</div>
        </div>
        <div className="glass-panel metric-card">
          <div className="metric-label">Called</div>
          <div className="metric-value">{totalCalled}</div>
        </div>
        <div className="glass-panel metric-card">
          <div className="metric-label">Qualified</div>
          <div className="metric-value" style={{color: '#22c55e'}}>{totalQualified}</div>
        </div>
        <div className="glass-panel metric-card">
          <div className="metric-label">Appointments</div>
          <div className="metric-value" style={{color: '#60a5fa'}}>{totalAppointments}</div>
        </div>
      </div>

      {/* Campaign Cards */}
      {activeCampaigns.length > 0 ? (
        <div style={{marginBottom: '2rem'}}>
          <h3 style={{color: '#94a3b8', fontSize: '0.8rem', textTransform: 'uppercase', letterSpacing: '1px', fontWeight: 600, marginBottom: '12px'}}>ACTIVE CAMPAIGNS</h3>
          <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: '1rem'}}>
            {activeCampaigns.map(c => (
              <div key={c.id} className="glass-panel" onClick={() => onCampaignClick(c)}
                style={{padding: '1.25rem', cursor: 'pointer', transition: 'border-color 0.2s', border: '1px solid rgba(255,255,255,0.05)'}}>
                <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '12px'}}>
                  <div>
                    <div style={{fontWeight: 700, color: '#e2e8f0', fontSize: '1rem', marginBottom: '4px'}}>{c.name}</div>
                    <span style={{background: 'rgba(6,182,212,0.2)', color: '#22d3ee', fontSize: '0.7rem', padding: '2px 8px', borderRadius: '10px', fontWeight: 600}}>
                      {c.product_name}
                    </span>
                  </div>
                  <span style={{padding: '2px 10px', borderRadius: '12px', fontSize: '0.7rem', fontWeight: 600,
                    color: '#22c55e', background: 'rgba(34,197,94,0.15)'}}>
                    active
                  </span>
                </div>
                <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr 1fr 1fr', gap: '8px', fontSize: '0.75rem'}}>
                  <div style={{textAlign: 'center'}}>
                    <div style={{color: '#94a3b8'}}>Leads</div>
                    <div style={{color: '#e2e8f0', fontWeight: 700, fontSize: '1.1rem'}}>{c.stats?.total || 0}</div>
                  </div>
                  <div style={{textAlign: 'center'}}>
                    <div style={{color: '#94a3b8'}}>Called</div>
                    <div style={{color: '#e2e8f0', fontWeight: 700, fontSize: '1.1rem'}}>{c.stats?.called || 0}</div>
                  </div>
                  <div style={{textAlign: 'center'}}>
                    <div style={{color: '#94a3b8'}}>Qualified</div>
                    <div style={{color: '#22c55e', fontWeight: 700, fontSize: '1.1rem'}}>{c.stats?.qualified || 0}</div>
                  </div>
                  <div style={{textAlign: 'center'}}>
                    <div style={{color: '#94a3b8'}}>Appts</div>
                    <div style={{color: '#60a5fa', fontWeight: 700, fontSize: '1.1rem'}}>{c.stats?.appointments || 0}</div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="glass-panel" style={{textAlign: 'center', padding: '3rem', color: '#64748b', marginBottom: '2rem'}}>
          No active campaigns. Go to the Campaigns tab to create one!
        </div>
      )}

      {/* Org Default Voice Settings */}
      {userRole === 'Admin' && (
        <div className="glass-panel" style={{padding: '1rem'}}>
          <div style={{display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap'}}>
            <span style={{fontSize: '0.8rem', color: '#64748b', fontWeight: 600}}>🔊 Org Default Voice:</span>
            <select className="form-input" value={activeVoiceProvider}
              onChange={e => { setActiveVoiceProvider(e.target.value); setActiveVoiceId(INDIAN_VOICES[e.target.value]?.[0]?.id || ''); }}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '100px'}}>
              <option value="elevenlabs">ElevenLabs</option>
              <option value="sarvam">Sarvam AI</option>
              <option value="smallest">Smallest AI</option>
            </select>
            <select className="form-input" value={activeVoiceId}
              onChange={e => setActiveVoiceId(e.target.value)}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '160px'}}>
              {(INDIAN_VOICES[activeVoiceProvider] || []).map(v => (
                <option key={v.id} value={v.id}>{v.name}</option>
              ))}
            </select>
            <select className="form-input" value={activeLanguage}
              onChange={e => setActiveLanguage(e.target.value)}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '90px'}}>
              {INDIAN_LANGUAGES.map(l => (
                <option key={l.code} value={l.code}>{l.name}</option>
              ))}
            </select>
            <button style={{background: 'linear-gradient(135deg, #8b5cf6, #6d28d9)', border: 'none', color: '#fff', fontSize: '0.75rem', padding: '6px 10px', borderRadius: '6px', cursor: 'pointer', whiteSpace: 'nowrap'}}
              onClick={async () => {
                if (!selectedOrg) return;
                await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/voice-settings`, {
                  method: 'PUT', headers: {'Content-Type': 'application/json'},
                  body: JSON.stringify({ tts_provider: activeVoiceProvider, tts_voice_id: activeVoiceId, tts_language: activeLanguage })
                });
                const vName = (INDIAN_VOICES[activeVoiceProvider] || []).find(v => v.id === activeVoiceId)?.name || activeVoiceId;
                setSavedVoiceName(vName);
              }}>💾 Save Default</button>
            {savedVoiceName && (
              <span style={{fontSize: '0.75rem', color: '#a78bfa'}}>✅ {savedVoiceName}</span>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
