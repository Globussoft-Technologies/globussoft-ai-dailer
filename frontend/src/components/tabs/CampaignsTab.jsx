import React, { useState, useEffect } from 'react';

export default function CampaignsTab({
  campaigns, fetchCampaigns, orgProducts, leads,
  apiFetch, API_URL, selectedOrg,
  onCampaignDial, onCampaignWebCall,
  handleViewTranscripts, handleNote,
  activeVoiceProvider, activeVoiceId, activeLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  dialingId, webCallActive
}) {
  const [view, setView] = useState('list'); // 'list' or 'detail'
  const [selectedCampaign, setSelectedCampaign] = useState(null);
  const [campaignLeads, setCampaignLeads] = useState([]);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showAddLeadsModal, setShowAddLeadsModal] = useState(false);
  const [createForm, setCreateForm] = useState({ name: '', product_id: '' });
  const [selectedLeadIds, setSelectedLeadIds] = useState([]);
  const [loading, setLoading] = useState(false);
  const [showCsvImportModal, setShowCsvImportModal] = useState(false);
  const [csvFile, setCsvFile] = useState(null);
  const [campVoice, setCampVoice] = useState({tts_provider: '', tts_voice_id: '', tts_language: ''});

  useEffect(() => { fetchCampaigns(); }, []);

  const fetchCampaignLeads = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/leads`);
      setCampaignLeads(await res.json());
    } catch(e) { setCampaignLeads([]); }
  };

  const fetchCampVoice = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/voice-settings`);
      if (res.ok) {
        const data = await res.json();
        setCampVoice({tts_provider: data.tts_provider || '', tts_voice_id: data.tts_voice_id || '', tts_language: data.tts_language || ''});
      } else {
        setCampVoice({tts_provider: '', tts_voice_id: '', tts_language: ''});
      }
    } catch(e) { setCampVoice({tts_provider: '', tts_voice_id: '', tts_language: ''}); }
  };

  const handleSaveCampVoice = async () => {
    if (!selectedCampaign) return;
    await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/voice-settings`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({tts_provider: campVoice.tts_provider, tts_voice_id: campVoice.tts_voice_id, tts_language: campVoice.tts_language})
    });
  };

  const handleResetCampVoice = async () => {
    if (!selectedCampaign) return;
    await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/voice-settings`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({tts_provider: '', tts_voice_id: '', tts_language: ''})
    });
    setCampVoice({tts_provider: '', tts_voice_id: '', tts_language: ''});
  };

  const handleViewCampaign = (campaign) => {
    setSelectedCampaign(campaign);
    setView('detail');
    fetchCampaignLeads(campaign.id);
    fetchCampVoice(campaign.id);
  };

  const handleBack = () => {
    setView('list');
    setSelectedCampaign(null);
    setCampaignLeads([]);
    fetchCampaigns();
  };

  const handleCreateCampaign = async (e) => {
    e.preventDefault();
    if (!createForm.name.trim()) return;
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/campaigns`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: createForm.name.trim(),
          product_id: createForm.product_id || null,
          org_id: selectedOrg?.id || null
        })
      });
      setCreateForm({ name: '', product_id: '' });
      setShowCreateModal(false);
      fetchCampaigns();
    } catch(e) { console.error(e); }
    setLoading(false);
  };

  const handleDeleteCampaign = async (campaignId) => {
    if (!window.confirm('Delete this campaign and remove all lead associations?')) return;
    try {
      await apiFetch(`${API_URL}/campaigns/${campaignId}`, { method: 'DELETE' });
      fetchCampaigns();
    } catch(e) { console.error(e); }
  };

  const handleToggleStatus = async (campaign) => {
    const nextStatus = campaign.status === 'active' ? 'paused' : 'active';
    try {
      await apiFetch(`${API_URL}/campaigns/${campaign.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: nextStatus })
      });
      fetchCampaigns();
      if (selectedCampaign?.id === campaign.id) {
        setSelectedCampaign({ ...campaign, status: nextStatus });
      }
    } catch(e) { console.error(e); }
  };

  const handleAddLeads = async () => {
    if (selectedLeadIds.length === 0) return;
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lead_ids: selectedLeadIds })
      });
      setSelectedLeadIds([]);
      setShowAddLeadsModal(false);
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
    } catch(e) { console.error(e); }
    setLoading(false);
  };

  const handleRemoveLead = async (leadId) => {
    try {
      await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads/${leadId}`, { method: 'DELETE' });
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
    } catch(e) { console.error(e); }
  };

  const handleLeadStatusChange = async (leadId, newStatus) => {
    try {
      await apiFetch(`${API_URL}/leads/${leadId}/status`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: newStatus })
      });
      fetchCampaignLeads(selectedCampaign.id);
    } catch(e) { console.error(e); }
  };

  const toggleLeadSelection = (leadId) => {
    setSelectedLeadIds(prev =>
      prev.includes(leadId) ? prev.filter(id => id !== leadId) : [...prev, leadId]
    );
  };

  const statusBadge = (status) => {
    const colors = { active: '#22c55e', paused: '#eab308', completed: '#6b7280' };
    const bg = { active: 'rgba(34,197,94,0.15)', paused: 'rgba(234,179,8,0.15)', completed: 'rgba(107,114,128,0.15)' };
    return (
      <span style={{
        padding: '2px 10px', borderRadius: '12px', fontSize: '0.75rem', fontWeight: 600,
        color: colors[status] || '#94a3b8', background: bg[status] || 'rgba(148,163,184,0.15)'
      }}>
        {status}
      </span>
    );
  };

  const getProductName = (productId) => {
    const p = orgProducts.find(p => p.id === productId);
    return p ? p.name : '';
  };

  const getCampaignStats = (campaign) => {
    const s = campaign.stats || {};
    return { total: s.total || 0, called: s.called || 0, qualified: s.qualified || 0, booked: s.appointments || 0 };
  };

  const handleCsvImport = async () => {
    if (!csvFile || !selectedCampaign) return;
    setLoading(true);
    try {
      const formData = new FormData();
      formData.append('file', csvFile);
      const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/import-csv`, {
        method: 'POST', body: formData
      });
      const data = await res.json();
      alert(`Imported ${data.imported} leads, ${data.added_to_campaign} added to campaign.${data.errors?.length ? '\nErrors: ' + data.errors.join(', ') : ''}`);
      setCsvFile(null);
      setShowCsvImportModal(false);
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
    } catch(e) { console.error(e); }
    setLoading(false);
  };

  // Available leads = org leads not already in this campaign
  const availableLeads = leads.filter(l => !campaignLeads.some(cl => cl.id === l.id));

  // ─── DETAIL VIEW ───
  if (view === 'detail' && selectedCampaign) {
    const stats = getCampaignStats(selectedCampaign);
    return (
      <div style={{padding: '1rem'}}>
        <button onClick={handleBack}
          style={{background: 'none', border: 'none', color: '#60a5fa', cursor: 'pointer', fontSize: '0.9rem', fontWeight: 600, marginBottom: '1rem', padding: 0}}>
          &larr; Back to Campaigns
        </button>

        <div style={{display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '1.5rem', flexWrap: 'wrap'}}>
          <h2 style={{margin: 0, color: '#e2e8f0'}}>{selectedCampaign.name}</h2>
          {selectedCampaign.product_id && (
            <span className="badge" style={{background: 'rgba(6,182,212,0.2)', color: '#22d3ee', fontSize: '0.75rem', padding: '2px 10px', borderRadius: '12px'}}>
              {getProductName(selectedCampaign.product_id)}
            </span>
          )}
          {statusBadge(selectedCampaign.status)}
        </div>

        <div className="metrics-grid" style={{marginBottom: '1.5rem'}}>
          <div className="glass-panel metric-card"><div className="metric-label">Total Leads</div><div className="metric-value">{stats.total}</div></div>
          <div className="glass-panel metric-card"><div className="metric-label">Called</div><div className="metric-value">{stats.called}</div></div>
          <div className="glass-panel metric-card"><div className="metric-label">Qualified</div><div className="metric-value">{stats.qualified}</div></div>
          <div className="glass-panel metric-card"><div className="metric-label">Appointments</div><div className="metric-value">{stats.booked}</div></div>
        </div>

        {/* Voice Settings */}
        <div className="glass-panel" style={{marginBottom: '1.5rem', padding: '12px 16px'}}>
          <div style={{display: 'flex', alignItems: 'center', gap: '12px', flexWrap: 'wrap'}}>
            <span style={{fontSize: '0.8rem', color: '#64748b', fontWeight: 600, whiteSpace: 'nowrap'}}>🔊 Campaign Voice Settings</span>
            <select className="form-input" value={campVoice.tts_provider}
              onChange={e => { const p = e.target.value; setCampVoice(v => ({...v, tts_provider: p, tts_voice_id: (INDIAN_VOICES[p] || [])[0]?.id || ''})); }}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '100px'}}>
              <option value="">-- Provider --</option>
              <option value="elevenlabs">ElevenLabs</option>
              <option value="sarvam">Sarvam AI</option>
              <option value="smallest">Smallest AI</option>
            </select>
            <select className="form-input" value={campVoice.tts_voice_id}
              onChange={e => setCampVoice(v => ({...v, tts_voice_id: e.target.value}))}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '160px'}}>
              <option value="">-- Voice --</option>
              {(INDIAN_VOICES[campVoice.tts_provider] || []).map(v => (
                <option key={v.id} value={v.id}>{v.name}</option>
              ))}
            </select>
            <select className="form-input" value={campVoice.tts_language}
              onChange={e => setCampVoice(v => ({...v, tts_language: e.target.value}))}
              style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '90px'}}>
              <option value="">-- Language --</option>
              {INDIAN_LANGUAGES.map(l => (
                <option key={l.code} value={l.code}>{l.name}</option>
              ))}
            </select>
            <button style={{background: 'linear-gradient(135deg, #8b5cf6, #6d28d9)', border: 'none', color: '#fff', fontSize: '0.75rem', padding: '6px 10px', borderRadius: '6px', cursor: 'pointer', whiteSpace: 'nowrap'}}
              onClick={handleSaveCampVoice}>Save</button>
            <button style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', fontSize: '0.75rem', padding: '6px 10px', borderRadius: '6px', cursor: 'pointer', whiteSpace: 'nowrap'}}
              onClick={handleResetCampVoice}>Reset to Org Default</button>
          </div>
          <div style={{fontSize: '0.7rem', color: '#a78bfa', marginTop: '6px'}}>
            {campVoice.tts_provider
              ? `Current: ${campVoice.tts_provider === 'elevenlabs' ? 'ElevenLabs' : campVoice.tts_provider === 'sarvam' ? 'Sarvam AI' : 'Smallest AI'} - ${(INDIAN_VOICES[campVoice.tts_provider] || []).find(v => v.id === campVoice.tts_voice_id)?.name || campVoice.tts_voice_id || 'none'}`
              : 'Using org default'}
          </div>
        </div>

        <div style={{display: 'flex', gap: '10px', marginBottom: '1rem', flexWrap: 'wrap'}}>
          <button className="btn-primary" onClick={() => { setSelectedLeadIds([]); setShowAddLeadsModal(true); }}>+ Add from CRM</button>
          <button className="btn-primary" style={{background: 'linear-gradient(135deg, #22d3ee, #06b6d4)'}}
            onClick={() => { setCsvFile(null); setShowCsvImportModal(true); }}>📤 Import CSV</button>
          <a href={`${API_URL}/leads/sample-csv`} download style={{color: '#94a3b8', fontSize: '0.8rem', textDecoration: 'underline', alignSelf: 'center'}}>📋 Sample CSV</a>
          {campaignLeads.some(l => (l.status || '').startsWith('Call Failed')) && (
            <button className="btn-call" style={{background: 'rgba(245,158,11,0.15)', color: '#f59e0b', borderColor: 'rgba(245,158,11,0.3)', fontSize: '0.85rem', padding: '8px 16px'}}
              onClick={async () => {
                const failedCount = campaignLeads.filter(l => (l.status || '').startsWith('Call Failed')).length;
                if (!window.confirm(`Redial ${failedCount} failed leads? (30s gap between calls to avoid spam)`)) return;
                try {
                  const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/redial-failed`, { method: 'POST' });
                  const data = await res.json();
                  alert(data.message || 'Redial started');
                } catch(e) { alert('Redial failed'); }
              }}>
              🔄 Redial Failed ({campaignLeads.filter(l => (l.status || '').startsWith('Call Failed')).length})
            </button>
          )}
          {campaignLeads.some(l => (l.status || '').toLowerCase() === 'new') && (
            <button className="btn-primary" style={{background: 'linear-gradient(135deg, #22c55e, #16a34a)', fontSize: '0.85rem', padding: '8px 16px'}}
              onClick={async () => {
                const newCount = campaignLeads.filter(l => (l.status || '').toLowerCase() === 'new').length;
                if (!window.confirm(`Dial ALL ${newCount} new leads? (30s gap between calls)`)) return;
                try {
                  const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/dial-all`, { method: 'POST' });
                  const data = await res.json();
                  alert(data.message || 'Dialing started');
                } catch(e) { alert('Dial failed'); }
              }}>
              📞 Dial All New ({campaignLeads.filter(l => (l.status || '').toLowerCase() === 'new').length})
            </button>
          )}
        </div>

        <div className="glass-panel" style={{overflowX: 'auto'}}>
          <table className="leads-table" style={{width: '100%'}}>
            <thead>
              <tr>
                <th>Name</th><th>Phone</th><th>Source</th><th>Status</th><th>Action</th>
              </tr>
            </thead>
            <tbody>
              {campaignLeads.length === 0 ? (
                <tr><td colSpan="5" style={{textAlign: 'center', color: '#64748b', padding: '2rem'}}>No leads in this campaign yet. Add some to start dialing!</td></tr>
              ) : campaignLeads.map(lead => (
                <React.Fragment key={lead.id}>
                <tr>
                  <td style={{fontWeight: 600}}>{lead.first_name} {lead.last_name}</td>
                  <td>{lead.phone}</td>
                  <td>{lead.source || '-'}</td>
                  <td>
                    <select className="form-input" value={lead.status || 'New'}
                      onChange={e => handleLeadStatusChange(lead.id, e.target.value)}
                      style={{width: 'auto', height: '30px', fontSize: '0.75rem', padding: '2px 6px'}}>
                      {['New','Contacted','Qualified','Appointment Set','Converted','Lost'].map(s => <option key={s} value={s}>{s}</option>)}
                    </select>
                  </td>
                  <td>
                    <div style={{display: 'flex', gap: '6px', flexWrap: 'wrap'}}>
                      <button className="btn-call"
                        onClick={() => onCampaignDial(lead, selectedCampaign.id)}
                        disabled={dialingId === lead.id}
                        style={{fontSize: '0.75rem', padding: '4px 10px', cursor: 'pointer'}}>
                        {dialingId === lead.id ? '📞 Dialing...' : '📞 Dial'}
                      </button>
                      <button className="btn-call"
                        onClick={() => onCampaignWebCall(lead, selectedCampaign.id)}
                        style={{
                          fontSize: '0.75rem', padding: '4px 10px', cursor: 'pointer',
                          borderColor: webCallActive === lead.id ? '#ef4444' : '#8b5cf6',
                          color: webCallActive === lead.id ? '#ef4444' : '#8b5cf6',
                          background: webCallActive === lead.id ? 'rgba(239,68,68,0.1)' : 'rgba(139,92,246,0.1)'
                        }}>
                        {webCallActive === lead.id ? '🔴 End Call' : '🌐 Sim Web Call'}
                      </button>
                      <button className="btn-call"
                        onClick={() => handleViewTranscripts(lead)}
                        style={{fontSize: '0.75rem', padding: '4px 10px', cursor: 'pointer',
                          background: lead.transcript_count > 0 ? 'rgba(34,197,94,0.15)' : 'rgba(99,102,241,0.08)',
                          color: lead.transcript_count > 0 ? '#22c55e' : '#64748b',
                          borderColor: lead.transcript_count > 0 ? 'rgba(34,197,94,0.3)' : 'rgba(99,102,241,0.15)',
                          fontWeight: lead.transcript_count > 0 ? 600 : 400}}>
                        {lead.transcript_count > 0 ? `📋 ${lead.transcript_count} Transcript${lead.transcript_count > 1 ? 's' : ''}` : '📋 No Calls'}
                        {lead.recording_count > 0 && ' 🔊'}
                      </button>
                      <button className="btn-call"
                        onClick={() => handleNote(lead)}
                        style={{fontSize: '0.75rem', padding: '4px 10px', cursor: 'pointer', background: 'rgba(168,85,247,0.15)', color: '#a855f7', borderColor: 'rgba(168,85,247,0.3)'}}>
                        📝 Note
                      </button>
                      <button onClick={() => handleRemoveLead(lead.id)}
                        style={{fontSize: '0.75rem', padding: '4px 10px', cursor: 'pointer',
                          background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                          color: '#fca5a5', borderRadius: '6px'}}>
                        Remove
                      </button>
                    </div>
                  </td>
                </tr>
                {lead.follow_up_note && (
                  <tr>
                    <td colSpan="5" style={{padding: '12px 24px', background: 'rgba(0,0,0,0.2)', borderLeft: '3px solid #6366f1'}}>
                      <div style={{fontSize: '0.8rem', color: '#94a3b8', marginBottom: '6px', textTransform: 'uppercase', letterSpacing: '1px', fontWeight: 600}}>AI Follow-Up Note</div>
                      <div style={{whiteSpace: 'pre-wrap', color: '#e2e8f0', fontSize: '0.85rem', lineHeight: 1.5}}>{lead.follow_up_note}</div>
                    </td>
                  </tr>
                )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>

        {/* Add Leads Modal */}
        {showAddLeadsModal && (
          <div className="modal-overlay" onClick={() => setShowAddLeadsModal(false)}>
            <div className="glass-panel" onClick={e => e.stopPropagation()}
              style={{maxWidth: '500px', width: '90%', maxHeight: '70vh', display: 'flex', flexDirection: 'column'}}>
              <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Add Leads to Campaign</h3>
              {availableLeads.length === 0 ? (
                <p style={{color: '#64748b'}}>All leads are already in this campaign.</p>
              ) : (
                <div style={{flex: 1, overflowY: 'auto', marginBottom: '1rem'}}>
                  {availableLeads.map(lead => (
                    <label key={lead.id} style={{display: 'flex', alignItems: 'center', gap: '10px', padding: '8px 4px', cursor: 'pointer', borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                      <input type="checkbox" checked={selectedLeadIds.includes(lead.id)}
                        onChange={() => toggleLeadSelection(lead.id)} />
                      <span style={{color: '#e2e8f0', fontWeight: 500}}>{lead.first_name} {lead.last_name}</span>
                      <span style={{color: '#64748b', fontSize: '0.8rem'}}>{lead.phone}</span>
                    </label>
                  ))}
                </div>
              )}
              <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                <button onClick={() => setShowAddLeadsModal(false)}
                  style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                  Cancel
                </button>
                <button className="btn-primary" onClick={handleAddLeads} disabled={loading || selectedLeadIds.length === 0}>
                  {loading ? 'Adding...' : `Add Selected (${selectedLeadIds.length})`}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* CSV Import Modal */}
        {showCsvImportModal && (
          <div className="modal-overlay" onClick={() => setShowCsvImportModal(false)}>
            <div className="glass-panel" onClick={e => e.stopPropagation()}
              style={{maxWidth: '450px', width: '90%'}}>
              <h3 style={{marginTop: 0, color: '#e2e8f0'}}>📤 Import Leads from CSV</h3>
              <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '1rem'}}>
                Upload a CSV with columns: first_name, last_name, phone, source. Leads will be created and added to this campaign.
              </p>
              <input type="file" accept=".csv" onChange={e => setCsvFile(e.target.files[0])}
                style={{marginBottom: '1rem', color: '#e2e8f0', fontSize: '0.85rem'}} />
              <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                <button onClick={() => setShowCsvImportModal(false)}
                  style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                  Cancel
                </button>
                <button className="btn-primary" onClick={handleCsvImport} disabled={loading || !csvFile}>
                  {loading ? 'Importing...' : 'Import & Add to Campaign'}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    );
  }

  // ─── LIST VIEW ───
  return (
    <div style={{padding: '1rem'}}>
      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem'}}>
        <h2 style={{margin: 0, color: '#e2e8f0'}}>📢 Campaigns</h2>
        <button className="btn-primary" onClick={() => setShowCreateModal(true)}>+ Create Campaign</button>
      </div>

      {campaigns.length === 0 ? (
        <div className="glass-panel" style={{textAlign: 'center', padding: '3rem', color: '#64748b'}}>
          No campaigns yet. Create one to start dialing!
        </div>
      ) : (
        <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: '1rem'}}>
          {campaigns.map(campaign => {
            const stats = getCampaignStats(campaign);
            return (
              <div key={campaign.id} className="glass-panel" style={{padding: '1.25rem', display: 'flex', flexDirection: 'column', gap: '10px'}}>
                <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start'}}>
                  <div>
                    <div style={{fontWeight: 700, fontSize: '1.05rem', color: '#e2e8f0', marginBottom: '6px'}}>{campaign.name}</div>
                    <div style={{display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap'}}>
                      {campaign.product_id && (
                        <span style={{background: 'rgba(6,182,212,0.2)', color: '#22d3ee', fontSize: '0.7rem', padding: '2px 8px', borderRadius: '10px', fontWeight: 600}}>
                          {getProductName(campaign.product_id)}
                        </span>
                      )}
                      {statusBadge(campaign.status || 'active')}
                    </div>
                  </div>
                  <button onClick={() => handleDeleteCampaign(campaign.id)}
                    style={{background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                      color: '#fca5a5', borderRadius: '6px', padding: '4px 8px', cursor: 'pointer', fontSize: '0.7rem'}}>
                    Delete
                  </button>
                </div>

                <div style={{display: 'flex', gap: '12px', fontSize: '0.75rem', color: '#94a3b8'}}>
                  <span>Total: <strong style={{color: '#e2e8f0'}}>{stats.total}</strong></span>
                  <span>Called: <strong style={{color: '#e2e8f0'}}>{stats.called}</strong></span>
                  <span>Qualified: <strong style={{color: '#22c55e'}}>{stats.qualified}</strong></span>
                  <span>Booked: <strong style={{color: '#60a5fa'}}>{stats.booked}</strong></span>
                </div>

                <button onClick={() => handleViewCampaign(campaign)}
                  style={{
                    marginTop: 'auto', background: 'rgba(96,165,250,0.1)', border: '1px solid rgba(96,165,250,0.3)',
                    color: '#60a5fa', padding: '8px 0', borderRadius: '8px', cursor: 'pointer', fontWeight: 600, fontSize: '0.85rem'
                  }}>
                  View Leads
                </button>
              </div>
            );
          })}
        </div>
      )}

      {/* Create Campaign Modal */}
      {showCreateModal && (
        <div className="modal-overlay" onClick={() => setShowCreateModal(false)}>
          <div className="glass-panel" onClick={e => e.stopPropagation()}
            style={{maxWidth: '450px', width: '90%'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Create New Campaign</h3>
            <form onSubmit={handleCreateCampaign}>
              <div style={{marginBottom: '1rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Campaign Name</label>
                <input className="form-input" placeholder="e.g. AdsGPT March Campaign"
                  value={createForm.name} onChange={e => setCreateForm({...createForm, name: e.target.value})}
                  style={{width: '100%'}} />
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Product</label>
                <select className="form-input" value={createForm.product_id}
                  onChange={e => setCreateForm({...createForm, product_id: e.target.value})}
                  style={{width: '100%'}}>
                  <option value="">-- Select Product --</option>
                  {orgProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </div>
              <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                <button type="button" onClick={() => setShowCreateModal(false)}
                  style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                  Cancel
                </button>
                <button type="submit" className="btn-primary" disabled={loading || !createForm.name.trim()}>
                  {loading ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
