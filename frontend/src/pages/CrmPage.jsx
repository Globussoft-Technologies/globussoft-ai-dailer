import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import CrmTab from '../components/tabs/CrmTab';
import LeadModals from '../components/modals/LeadModals';
import DocumentVault from '../components/modals/DocumentVault';
import TranscriptModal from '../components/modals/TranscriptModal';
import EmailDraftModal from '../components/modals/EmailDraftModal';

export default function CrmPage({
  apiFetch, API_URL, selectedOrg, orgTimezone,
  dialingId, setDialingId, webCallActive,
  handleDial, handleWebCall,
  campaigns,
  activeVoiceProvider, setActiveVoiceProvider,
  activeVoiceId, setActiveVoiceId,
  activeLanguage, setActiveLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  savedVoiceName, setSavedVoiceName,
  userRole, authToken
}) {
  const navigate = useNavigate();
  // Lead State
  const [leads, setLeads] = useState([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [formData, setFormData] = useState({ first_name: '', last_name: '', phone: '', source: 'Manual Entry' });
  const [loading, setLoading] = useState(false);

  // Edit Lead State
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [editingLead, setEditingLead] = useState(null);
  const [editFormData, setEditFormData] = useState({ first_name: '', last_name: '', phone: '', source: '' });

  // Note State
  const [noteLead, setNoteLead] = useState(null);
  const [noteText, setNoteText] = useState('');

  // Document Vault State
  const [activeLeadDocs, setActiveLeadDocs] = useState(null);
  const [docs, setDocs] = useState([]);
  const [docFormData, setDocFormData] = useState({ file_name: '', file_url: '' });

  // GenAI Email Modal State
  const [emailDraft, setEmailDraft] = useState(null);

  // Call Transcript State
  const [transcriptLead, setTranscriptLead] = useState(null);
  const [transcripts, setTranscripts] = useState([]);

  useEffect(() => {
    fetchLeads();
  }, []);

  const fetchLeads = async () => {
    try {
      const res = await apiFetch(`${API_URL}/leads`);
      const data = await res.json();
      setLeads(data);
    } catch (e) {
      console.error("Make sure FastAPI is running with CORS enabled!", e);
    }
  };

  const handleSearch = async (e) => {
    const query = e.target.value;
    setSearchQuery(query);
    if (query.trim().length >= 2) {
      try {
        const res = await apiFetch(`${API_URL}/leads/search?q=${encodeURIComponent(query)}`);
        setLeads(await res.json());
      } catch(e) {}
    } else if (query.trim().length === 0) {
      fetchLeads();
    }
  };

  const handleCreateLead = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/leads`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(formData)
      });
      setFormData({ first_name: '', last_name: '', phone: '', source: 'Manual Entry' });
      setIsModalOpen(false);
      fetchLeads();
    } catch(e) {
      console.error(e);
    }
    setLoading(false);
  };

  const handleStatusChange = async (leadId, newStatus) => {
    try {
      await apiFetch(`${API_URL}/leads/${leadId}/status`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: newStatus })
      });
      fetchLeads();
    } catch (e) { console.error(e); }
  };

  const handleEditLead = (lead) => {
    setEditingLead(lead);
    setEditFormData({
      first_name: lead.first_name || '',
      last_name: lead.last_name || '',
      phone: lead.phone || '',
      source: lead.source || 'Manual Entry'
    });
    setEditModalOpen(true);
  };

  const handleSaveEdit = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/${editingLead.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editFormData)
      });
      const data = await res.json();
      if (!res.ok || data.status === 'error') {
        throw new Error(data.message || 'Error updating lead details');
      }
      setEditModalOpen(false);
      setEditingLead(null);
      fetchLeads();
    } catch (e) {
      alert(e.message);
      console.error('Error updating lead', e);
    }
    setLoading(false);
  };

  const handleDeleteLead = async (lead) => {
    if (!window.confirm(`Are you sure you want to delete ${lead.first_name} ${lead.last_name}?`)) return;
    try {
      await apiFetch(`${API_URL}/leads/${lead.id}`, { method: 'DELETE' });
      fetchLeads();
    } catch (e) {
      console.error('Error deleting lead', e);
    }
  };

  const handleOpenDocs = async (lead) => {
    setActiveLeadDocs(lead);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/documents`);
      setDocs(await res.json());
    } catch(e) {}
  };

  const handleUploadDoc = async (e) => {
    e.preventDefault();
    try {
      await apiFetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(docFormData)
      });
      setDocFormData({ file_name: '', file_url: '' });
      const res = await apiFetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`);
      setDocs(await res.json());
    } catch(e) { console.error(e); }
  };

  const handleNote = (lead) => {
    setNoteLead(lead);
    setNoteText(lead.follow_up_note || '');
  };

  const handleSaveNote = async () => {
    if (!noteLead) return;
    try {
      await apiFetch(`${API_URL}/leads/${noteLead.id}/notes`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: noteText })
      });
      fetchLeads();
      setNoteLead(null);
    } catch(e) {
      console.error("Error saving note", e);
    }
  };

  const handleDraftEmail = async (lead) => {
    setDialingId(lead.id); // Reuse the dialing spinner temporarily
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/draft-email`);
      const data = await res.json();
      setEmailDraft(data);
    } catch(e) {
      console.error("Error generating email", e);
    }
    setDialingId(null);
  };

  const handleViewTranscripts = async (lead) => {
    setTranscriptLead(lead);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/transcripts`);
      setTranscripts(await res.json());
    } catch(e) { setTranscripts([]); }
  };

  return (
    <>
      <CrmTab
        searchQuery={searchQuery} handleSearch={handleSearch} setIsModalOpen={setIsModalOpen}
        userRole={userRole} leads={leads} API_URL={API_URL} authToken={authToken} fetchLeads={fetchLeads}
        activeVoiceProvider={activeVoiceProvider} setActiveVoiceProvider={setActiveVoiceProvider}
        activeVoiceId={activeVoiceId} setActiveVoiceId={setActiveVoiceId}
        activeLanguage={activeLanguage} setActiveLanguage={setActiveLanguage}
        INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
        selectedOrg={selectedOrg} apiFetch={apiFetch}
        savedVoiceName={savedVoiceName} setSavedVoiceName={setSavedVoiceName}
        handleStatusChange={handleStatusChange} handleEditLead={handleEditLead}
        handleDeleteLead={handleDeleteLead} handleOpenDocs={handleOpenDocs}
        handleViewTranscripts={handleViewTranscripts} handleNote={handleNote}
        handleDraftEmail={handleDraftEmail} dialingId={dialingId}
        webCallActive={webCallActive} handleWebCall={handleWebCall} handleDial={handleDial}
        campaigns={campaigns}
        onCampaignClick={() => navigate('/campaigns')}
      />

      <LeadModals
        isModalOpen={isModalOpen} setIsModalOpen={setIsModalOpen}
        handleCreateLead={handleCreateLead} formData={formData}
        setFormData={setFormData} loading={loading}
        editModalOpen={editModalOpen} setEditModalOpen={setEditModalOpen}
        editingLead={editingLead} handleSaveEdit={handleSaveEdit}
        editFormData={editFormData} setEditFormData={setEditFormData}
      />
      <DocumentVault
        activeLeadDocs={activeLeadDocs} setActiveLeadDocs={setActiveLeadDocs}
        handleUploadDoc={handleUploadDoc} docFormData={docFormData}
        setDocFormData={setDocFormData} docs={docs} orgTimezone={orgTimezone}
      />
      <TranscriptModal
        transcriptLead={transcriptLead} setTranscriptLead={setTranscriptLead}
        transcripts={transcripts} orgTimezone={orgTimezone}
      />
      <EmailDraftModal
        emailDraft={emailDraft} setEmailDraft={setEmailDraft}
      />

      {/* Note Modal */}
      {noteLead && (
        <div className="modal-overlay" onClick={() => setNoteLead(null)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()} style={{maxWidth: '520px'}}>
            <h2 style={{marginTop: 0, marginBottom: '0.5rem'}}>📝 Quick Note</h2>
            <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '1.5rem'}}>
              {noteLead.first_name} {noteLead.last_name} — {noteLead.phone}
            </p>
            <textarea className="form-input" rows={5} value={noteText}
              onChange={e => setNoteText(e.target.value)}
              placeholder="Type your follow-up note here..."
              style={{width: '100%', minHeight: '120px', resize: 'vertical', fontSize: '0.9rem', lineHeight: 1.5}} />
            <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '1.5rem'}}>
              <button onClick={() => setNoteLead(null)}
                style={{background: 'transparent', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', padding: '8px 18px', borderRadius: '8px', cursor: 'pointer'}}>
                Cancel
              </button>
              <button className="btn-primary" onClick={handleSaveNote}>
                Save Note
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
