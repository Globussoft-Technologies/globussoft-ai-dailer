import React from 'react';

export default function CrmTab({
  searchQuery, handleSearch, setIsModalOpen, userRole, leads, API_URL, authToken, fetchLeads,
  activeVoiceProvider, setActiveVoiceProvider, activeVoiceId, setActiveVoiceId,
  activeLanguage, setActiveLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  selectedOrg, apiFetch, savedVoiceName, setSavedVoiceName,
  handleStatusChange, handleEditLead, handleDeleteLead, handleOpenDocs, handleViewTranscripts,
  handleNote, handleDraftEmail, dialingId, webCallActive, handleWebCall, handleDial
}) {
  return (
    <div className="crm-container">
      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', marginBottom: '1rem'}}>
        <h2 style={{marginTop: 0, marginBottom: 0}}>Deal Pipeline</h2>
        <div style={{position: 'relative'}}>
          <input 
            type="text" 
            className="form-input" 
            placeholder="🔍 Search Leads by Name or Phone..." 
            value={searchQuery}
            onChange={handleSearch}
            data-testid="lead-search"
            style={{width: '320px', borderRadius: '30px', paddingLeft: '20px', marginBottom: 0, background: 'rgba(15, 23, 42, 0.6)'}}
          />
        </div>

        <div style={{display: 'flex', gap: '10px', marginLeft: '1rem'}}>
          <button data-testid="add-lead-btn" className="btn-primary" onClick={() => setIsModalOpen(true)}>
            + Add Lead
          </button>
          {userRole === 'Admin' && (<>
            <button data-testid="export-csv-btn" className="btn-call" style={{borderColor: '#22c55e', color: '#22c55e', padding: '0 16px', height: '40px', background: 'rgba(34, 197, 94, 0.1)', cursor: 'pointer'}} onClick={() => window.open(`${API_URL}/leads/export`, '_blank')}>
              📥 Export CSV
            </button>
            <button className="btn-call" style={{borderColor: '#3b82f6', color: '#3b82f6', padding: '0 16px', height: '40px', background: 'rgba(59, 130, 246, 0.1)', cursor: 'pointer'}} onClick={() => document.getElementById('csv-import-input').click()}>
              📤 Import CSV
            </button>
            <input id="csv-import-input" type="file" accept=".csv" style={{display: 'none'}} onChange={async (e) => {
              const f = e.target.files[0]; if (!f) return;
              const fd = new FormData(); fd.append('file', f);
              try {
                // BUGFIX: using authToken instead of undefined token
                const r = await fetch(`${API_URL}/leads/import-csv`, {method: 'POST', headers: {'Authorization': `Bearer ${authToken}`}, body: fd});
                const d = await r.json();
                alert(`✅ Imported ${d.imported} leads` + (d.errors?.length ? `\n⚠️ Errors:\n${d.errors.join('\n')}` : ''));
                fetchLeads();
              } catch (err) { alert('Import failed: ' + err.message); }
              e.target.value = '';
            }} />
            <a href={`${API_URL}/leads/sample-csv`} style={{color: '#94a3b8', fontSize: '13px', alignSelf: 'center', textDecoration: 'underline', cursor: 'pointer'}}>📋 Sample CSV</a>
          </>)}
        </div>
      </div>

      {userRole === 'Admin' && (
        <div className="metrics-grid">
          <div className="glass-panel metric-card">
            <div className="metric-label">Total Leads</div>
            <div className="metric-value">{leads.length}</div>
          </div>
          <div className="glass-panel metric-card">
            <div className="metric-label">Active Calls</div>
            <div className="metric-value">0</div>
          </div>
          <div className="glass-panel metric-card">
            <div className="metric-label">Success Rate</div>
            <div className="metric-value">94%</div>
          </div>
        </div>
      )}

      <div className="glass-panel" style={{overflowX: 'auto'}}>
        <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', flexWrap: 'wrap', gap: '12px'}}>
          <h2 style={{marginTop: 0, marginBottom: 0, fontSize: '1.25rem', fontWeight: 600}}>Campaign Leads</h2>
          <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
            <span style={{fontSize: '0.8rem', color: '#64748b', fontWeight: 600}}>🔊 Voice:</span>
            <select className="form-input" value={activeVoiceProvider}
              onChange={e => { setActiveVoiceProvider(e.target.value); setActiveVoiceId(INDIAN_VOICES[e.target.value]?.[0]?.id || ''); }}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '100px'}}>
              <option value="elevenlabs">ElevenLabs</option>
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
          </div>
          {savedVoiceName && (
            <div style={{fontSize: '0.75rem', color: '#a78bfa', marginTop: '4px', textAlign: 'right'}}>✅ Default voice: <strong>{savedVoiceName}</strong></div>
          )}
        </div>
        <table className="leads-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Phone</th>
              <th>Source</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {leads.length === 0 ? (
              <tr><td colSpan="5" style={{textAlign: "center", padding: "3rem", color: '#94a3b8'}}>No leads found. Click 'Add Lead' to populate!</td></tr>
            ) : leads.map(lead => (
              <React.Fragment key={lead.id}>
                <tr>
                  <td style={{fontWeight: 500}}>{lead.first_name} {lead.last_name}</td>
                  <td style={{fontFamily: 'SFMono-Regular, Consolas, monospace', color: '#cbd5e1'}}>{lead.phone}</td>
                  <td><span className="badge">{lead.source}</span></td>
                  <td>
                    <select 
                      value={lead.status || 'new'} 
                      onChange={(e) => handleStatusChange(lead.id, e.target.value)}
                      style={{background: 'rgba(0,0,0,0.3)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', padding: '4px 8px', borderRadius: '4px'}}
                    >
                      <option value="new">New</option>
                      <option value="Warm">Warm</option>
                      <option value="Summarized">Summarized</option>
                      <option value="Closed">Closed</option>
                    </select>
                  </td>
                  <td>
                    <div style={{display: 'flex', gap: '8px'}}>
                      <button 
                        className="btn-call" 
                        style={{background: 'rgba(250, 204, 21, 0.15)', color: '#facc15', borderColor: 'rgba(250, 204, 21, 0.3)', padding: '4px 10px', fontSize: '0.8rem'}}
                        onClick={() => handleEditLead(lead)}
                      >
                        ✏️ Edit
                      </button>
                      <button 
                        className="btn-call" 
                        style={{background: 'rgba(239, 68, 68, 0.15)', color: '#ef4444', borderColor: 'rgba(239, 68, 68, 0.3)', padding: '4px 10px', fontSize: '0.8rem'}}
                        onClick={() => handleDeleteLead(lead)}
                      >
                        🗑️
                      </button>
                      <button 
                        className="btn-call" 
                        style={{background: 'rgba(56, 189, 248, 0.15)', color: '#38bdf8', borderColor: 'rgba(56, 189, 248, 0.3)'}}
                        onClick={() => handleOpenDocs(lead)}
                      >
                        📁 Docs
                      </button>
                      <button 
                        className="btn-call" 
                        style={{background: 'rgba(99, 102, 241, 0.15)', color: '#818cf8', borderColor: 'rgba(99, 102, 241, 0.3)'}}
                        onClick={() => handleViewTranscripts(lead)}
                      >
                        📋 Transcript
                      </button>
                      <button 
                        className="btn-call" 
                        style={{background: 'rgba(168, 85, 247, 0.15)', color: '#a855f7', borderColor: 'rgba(168, 85, 247, 0.3)'}}
                        onClick={() => handleNote(lead)}
                      >
                        📝 Note
                      </button>
                      <button 
                        className="btn-call" 
                        style={{background: 'linear-gradient(135deg, rgba(245, 158, 11, 0.15), rgba(220, 38, 38, 0.15))', color: '#f59e0b', borderColor: 'rgba(245, 158, 11, 0.3)'}}
                        onClick={() => handleDraftEmail(lead)}
                        disabled={dialingId === lead.id}
                      >
                        {dialingId === lead.id ? 'Thinking...' : '📧 AI Email'}
                      </button>
                      <button 
                        className="btn-call" 
                        style={{
                          background: webCallActive === lead.id ? '#ef4444' : 'linear-gradient(135deg, rgba(34, 211, 238, 0.15), rgba(14, 165, 233, 0.15))', 
                          color: webCallActive === lead.id ? '#ffffff' : '#38bdf8', 
                          borderColor: webCallActive === lead.id ? '#ef4444' : 'rgba(34, 211, 238, 0.3)',
                          boxShadow: webCallActive === lead.id ? '0 0 12px rgba(239, 68, 68, 0.6)' : 'none',
                          fontWeight: webCallActive === lead.id ? 700 : 500
                        }}
                        onClick={() => handleWebCall(lead)}
                      >
                        {webCallActive === lead.id ? '🛑 End Live Sim' : '🎙️ Sim Web Call'}
                      </button>
                      <button 
                        className="btn-call" 
                        onClick={() => handleDial(lead)}
                        disabled={dialingId === lead.id}
                      >
                        {dialingId === lead.id ? 'Dialing...' : '📞 Exotel'}
                      </button>
                    </div>
                  </td>
                </tr>
                {lead.follow_up_note && (
                  <tr>
                    <td colSpan="5" style={{padding: '16px 24px', background: 'rgba(0,0,0,0.2)', borderLeft: '3px solid #6366f1'}}>
                      <div style={{fontSize: '0.85rem', color: '#94a3b8', marginBottom: '8px', textTransform: 'uppercase', letterSpacing: '1px', fontWeight: 600}}>AI Follow-Up Note</div>
                      <div style={{whiteSpace: 'pre-wrap', color: '#e2e8f0', fontSize: '0.9rem', lineHeight: 1.6}}>{lead.follow_up_note}</div>
                    </td>
                  </tr>
                )}
              </React.Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
