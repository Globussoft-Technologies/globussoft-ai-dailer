import React, { useState, useEffect } from 'react';
import CampaignDetail from '../campaigns/CampaignDetail';
import CampaignModals from '../campaigns/CampaignModals';
import { CAMPAIGN_TEMPLATES } from '../../constants/campaignTemplates';

export default function CampaignsTab({
  campaigns, fetchCampaigns, orgProducts, leads,
  apiFetch, API_URL, selectedOrg,
  onCampaignDial, onCampaignWebCall,
  handleViewTranscripts, handleNote,
  activeVoiceProvider, activeVoiceId, activeLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  dialingId, webCallActive, orgTimezone
}) {
  const [view, setView] = useState('list'); // 'list' or 'detail'
  const [selectedCampaign, setSelectedCampaign] = useState(null);
  const [campaignLeads, setCampaignLeads] = useState([]);
  const [callLog, setCallLog] = useState([]);
  const [detailTab, setDetailTab] = useState('leads'); // 'leads' or 'calllog'
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showAddLeadsModal, setShowAddLeadsModal] = useState(false);
  const [editLead, setEditLead] = useState(null);
  const [editForm, setEditForm] = useState({ first_name: '', last_name: '', phone: '', source: '' });
  const [createForm, setCreateForm] = useState({ name: '', product_id: '', lead_source: '' });
  const [selectedLeadIds, setSelectedLeadIds] = useState([]);
  const [loading, setLoading] = useState(false);
  const [showCsvImportModal, setShowCsvImportModal] = useState(false);
  const [csvFile, setCsvFile] = useState(null);
  const [liveEvents, setLiveEvents] = useState([]);
  const [showEditCampaignModal, setShowEditCampaignModal] = useState(false);
  const [editCampaignForm, setEditCampaignForm] = useState({ name: '', product_id: '', lead_source: '' });
  const [selectedTemplate, setSelectedTemplate] = useState(null);
  const eventSourceRef = React.useRef(null);
  const [campVoice, setCampVoice] = useState({tts_provider: '', tts_voice_id: '', tts_language: ''});

  useEffect(() => { fetchCampaigns(); }, []);

  const fetchCampaignLeads = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/leads`);
      setCampaignLeads(await res.json());
    } catch(e) { setCampaignLeads([]); }
  };

  const fetchCallLog = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/call-log`);
      setCallLog(await res.json());
    } catch(e) { setCallLog([]); }
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
    fetchCallLog(campaign.id);
    fetchCampVoice(campaign.id);
    startEventStream(campaign.id);
    setDetailTab('leads');
  };

  const handleBack = () => {
    stopEventStream();
    setView('list');
    setSelectedCampaign(null);
    setCampaignLeads([]);
    setLiveEvents([]);
    fetchCampaigns();
  };

  const startEventStream = (campaignId) => {
    stopEventStream();
    const token = localStorage.getItem('authToken');
    if (!token) return;
    const es = new EventSource(`${API_URL}/campaign-events?token=${token}&campaign_id=${campaignId}`);
    es.onmessage = (e) => setLiveEvents(prev => [...prev.slice(-49), e.data]);
    es.onerror = () => es.close();
    eventSourceRef.current = es;
  };

  const stopEventStream = () => {
    if (eventSourceRef.current) { eventSourceRef.current.close(); eventSourceRef.current = null; }
  };

  const handleCreateCampaign = async (e) => {
    e.preventDefault();
    if (!createForm.name.trim()) return;
    setLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/campaigns`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: createForm.name.trim(),
          product_id: createForm.product_id || null,
          lead_source: createForm.lead_source || null,
          org_id: selectedOrg?.id || null
        })
      });
      const newCampaign = await res.json();

      // Apply template settings if one was selected
      if (selectedTemplate && newCampaign?.id) {
        // Set voice settings
        await apiFetch(`${API_URL}/campaigns/${newCampaign.id}/voice-settings`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            tts_provider: selectedTemplate.tts_provider,
            tts_voice_id: selectedTemplate.tts_voice_id,
            tts_language: selectedTemplate.language
          })
        });

        // Set product prompt if a product was linked
        const productId = createForm.product_id || newCampaign.product_id;
        if (productId) {
          await apiFetch(`${API_URL}/products/${productId}/prompt`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              agent_persona: selectedTemplate.agent_persona,
              call_flow_instructions: selectedTemplate.call_flow_instructions
            })
          });
        }
      }

      setCreateForm({ name: '', product_id: '', lead_source: '' });
      setSelectedTemplate(null);
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

  const handleEditCampaign = (campaign) => {
    setEditCampaignForm({
      id: campaign.id,
      name: campaign.name || '',
      product_id: campaign.product_id || '',
      lead_source: campaign.lead_source || ''
    });
    setShowEditCampaignModal(true);
  };

  const handleSaveEditCampaign = async (e) => {
    e.preventDefault();
    if (!editCampaignForm.name.trim()) return;
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/campaigns/${editCampaignForm.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: editCampaignForm.name.trim(),
          product_id: editCampaignForm.product_id ? parseInt(editCampaignForm.product_id) : null,
          lead_source: editCampaignForm.lead_source || null
        })
      });
      setShowEditCampaignModal(false);
      fetchCampaigns();
      if (selectedCampaign?.id === editCampaignForm.id) {
        setSelectedCampaign(prev => ({
          ...prev,
          name: editCampaignForm.name.trim(),
          product_id: editCampaignForm.product_id ? parseInt(editCampaignForm.product_id) : prev.product_id,
          lead_source: editCampaignForm.lead_source || null
        }));
      }
    } catch(e) { console.error(e); }
    setLoading(false);
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

  const handleEditLead = (lead) => {
    setEditLead(lead);
    setEditForm({ first_name: lead.first_name || '', last_name: lead.last_name || '', phone: lead.phone || '', source: lead.source || '' });
  };

  const handleSaveEdit = async () => {
    if (!editLead) return;
    try {
      await apiFetch(`${API_URL}/leads/${editLead.id}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editForm)
      });
      setEditLead(null);
      fetchCampaignLeads(selectedCampaign.id);
    } catch(e) { alert('Save failed'); }
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
    return (
      <>
        <CampaignDetail
          selectedCampaign={selectedCampaign}
          setSelectedCampaign={setSelectedCampaign}
          campaignLeads={campaignLeads}
          callLog={callLog}
          detailTab={detailTab}
          setDetailTab={setDetailTab}
          handleBack={handleBack}
          fetchCampaignLeads={fetchCampaignLeads}
          fetchCallLog={fetchCallLog}
          fetchCampaigns={fetchCampaigns}
          statusBadge={statusBadge}
          getProductName={getProductName}
          getCampaignStats={getCampaignStats}
          campVoice={campVoice}
          setCampVoice={setCampVoice}
          handleSaveCampVoice={handleSaveCampVoice}
          handleResetCampVoice={handleResetCampVoice}
          INDIAN_VOICES={INDIAN_VOICES}
          INDIAN_LANGUAGES={INDIAN_LANGUAGES}
          liveEvents={liveEvents}
          setLiveEvents={setLiveEvents}
          handleLeadStatusChange={handleLeadStatusChange}
          handleEditLead={handleEditLead}
          handleRemoveLead={handleRemoveLead}
          handleViewTranscripts={handleViewTranscripts}
          handleNote={handleNote}
          onCampaignDial={onCampaignDial}
          onCampaignWebCall={onCampaignWebCall}
          dialingId={dialingId}
          webCallActive={webCallActive}
          setSelectedLeadIds={setSelectedLeadIds}
          setShowAddLeadsModal={setShowAddLeadsModal}
          setShowCsvImportModal={setShowCsvImportModal}
          setCsvFile={setCsvFile}
          apiFetch={apiFetch}
          API_URL={API_URL}
          orgTimezone={orgTimezone}
          handleEditCampaign={handleEditCampaign}
        />
        <CampaignModals
          showCreateModal={false}
          setShowCreateModal={setShowCreateModal}
          createForm={createForm}
          setCreateForm={setCreateForm}
          handleCreateCampaign={handleCreateCampaign}
          loading={loading}
          orgProducts={orgProducts}
          selectedTemplate={selectedTemplate}
          setSelectedTemplate={setSelectedTemplate}
          showAddLeadsModal={showAddLeadsModal}
          setShowAddLeadsModal={setShowAddLeadsModal}
          availableLeads={availableLeads}
          selectedLeadIds={selectedLeadIds}
          toggleLeadSelection={toggleLeadSelection}
          handleAddLeads={handleAddLeads}
          showCsvImportModal={showCsvImportModal}
          setShowCsvImportModal={setShowCsvImportModal}
          csvFile={csvFile}
          setCsvFile={setCsvFile}
          handleCsvImport={handleCsvImport}
          editLead={editLead}
          setEditLead={setEditLead}
          editForm={editForm}
          setEditForm={setEditForm}
          handleSaveEdit={handleSaveEdit}
          showEditCampaignModal={showEditCampaignModal}
          setShowEditCampaignModal={setShowEditCampaignModal}
          editCampaignForm={editCampaignForm}
          setEditCampaignForm={setEditCampaignForm}
          handleSaveEditCampaign={handleSaveEditCampaign}
        />
      </>
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
                  <div style={{display: 'flex', gap: '6px'}}>
                    <button onClick={(e) => { e.stopPropagation(); handleEditCampaign(campaign); }}
                      style={{background: 'rgba(250,204,21,0.1)', border: '1px solid rgba(250,204,21,0.3)',
                        color: '#facc15', borderRadius: '6px', padding: '4px 8px', cursor: 'pointer', fontSize: '0.7rem'}}>
                      Edit
                    </button>
                    <button onClick={() => handleDeleteCampaign(campaign.id)}
                      style={{background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                        color: '#fca5a5', borderRadius: '6px', padding: '4px 8px', cursor: 'pointer', fontSize: '0.7rem'}}>
                      Delete
                    </button>
                  </div>
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

      <CampaignModals
        showCreateModal={showCreateModal}
        setShowCreateModal={setShowCreateModal}
        createForm={createForm}
        setCreateForm={setCreateForm}
        handleCreateCampaign={handleCreateCampaign}
        loading={loading}
        orgProducts={orgProducts}
        selectedTemplate={selectedTemplate}
        setSelectedTemplate={setSelectedTemplate}
        showAddLeadsModal={false}
        setShowAddLeadsModal={setShowAddLeadsModal}
        availableLeads={availableLeads}
        selectedLeadIds={selectedLeadIds}
        toggleLeadSelection={toggleLeadSelection}
        handleAddLeads={handleAddLeads}
        showCsvImportModal={false}
        setShowCsvImportModal={setShowCsvImportModal}
        csvFile={csvFile}
        setCsvFile={setCsvFile}
        handleCsvImport={handleCsvImport}
        editLead={null}
        setEditLead={setEditLead}
        editForm={editForm}
        setEditForm={setEditForm}
        handleSaveEdit={handleSaveEdit}
        showEditCampaignModal={showEditCampaignModal}
        setShowEditCampaignModal={setShowEditCampaignModal}
        editCampaignForm={editCampaignForm}
        setEditCampaignForm={setEditCampaignForm}
        handleSaveEditCampaign={handleSaveEditCampaign}
      />
    </div>
  );
}
