import React, { useState, useEffect } from 'react';
import CampaignsTab from '../components/tabs/CampaignsTab';
import TranscriptModal from '../components/modals/TranscriptModal';

export default function CampaignsPage({
  apiFetch, API_URL, selectedOrg, orgTimezone, orgProducts,
  dialingId, webCallActive,
  handleCampaignDial, handleCampaignWebCall,
  activeVoiceProvider, activeVoiceId, activeLanguage,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  campaigns, fetchCampaigns
}) {
  // Leads for adding to campaigns (the global leads pool)
  const [leads, setLeads] = useState([]);

  // Note state (for campaign lead notes)
  const [noteLead, setNoteLead] = useState(null);
  const [noteText, setNoteText] = useState('');
  const [noteSaving, setNoteSaving] = useState(false);

  // Transcript state (for campaign lead transcripts)
  const [transcriptLead, setTranscriptLead] = useState(null);
  const [transcripts, setTranscripts] = useState([]);

  useEffect(() => {
    fetchLeads();
  }, []);

  const fetchLeads = async () => {
    try {
      const res = await apiFetch(`${API_URL}/leads`);
      setLeads(await res.json());
    } catch(e) {}
  };

  const handleViewTranscripts = async (lead) => {
    setTranscriptLead(lead);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/transcripts`);
      if (!res.ok) { setTranscripts([]); return; }
      const data = await res.json();
      setTranscripts(Array.isArray(data) ? data : []);
    } catch(e) { setTranscripts([]); }
  };

  const handleNote = (lead) => {
    setNoteLead(lead);
    setNoteText(lead.follow_up_note || '');
  };

  const handleSaveNote = async () => {
    if (!noteLead) return;
    const trimmed = noteText.trim();
    if (!trimmed) { alert('Note cannot be empty'); return; }
    setNoteSaving(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/${noteLead.id}/notes`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: trimmed })
      });
      if (!res.ok) {
        let msg = `Failed to save note (HTTP ${res.status})`;
        try { const data = await res.json(); if (data?.error || data?.detail) msg = data.error || data.detail; } catch(_) {}
        alert(msg);
        return;
      }
      setNoteLead(null);
      setNoteText('');
    } catch(e) {
      alert('Failed to save note: ' + (e?.message || 'network error'));
    } finally {
      setNoteSaving(false);
    }
  };

  return (
    <>
      <CampaignsTab
        campaigns={campaigns} fetchCampaigns={fetchCampaigns}
        orgProducts={orgProducts} leads={leads}
        apiFetch={apiFetch} API_URL={API_URL} selectedOrg={selectedOrg}
        onCampaignDial={handleCampaignDial} onCampaignWebCall={handleCampaignWebCall}
        activeVoiceProvider={activeVoiceProvider} activeVoiceId={activeVoiceId}
        activeLanguage={activeLanguage}
        INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
        dialingId={dialingId} webCallActive={webCallActive}
        handleViewTranscripts={handleViewTranscripts} handleNote={handleNote}
        orgTimezone={orgTimezone}
      />

      <TranscriptModal
        transcriptLead={transcriptLead} setTranscriptLead={setTranscriptLead}
        transcripts={transcripts} orgTimezone={orgTimezone}
      />

      {/* Note Modal for campaign leads */}
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
              <button className="btn-primary" onClick={handleSaveNote}
                disabled={noteSaving || !noteText.trim()}
                style={{opacity: (noteSaving || !noteText.trim()) ? 0.5 : 1, cursor: (noteSaving || !noteText.trim()) ? 'not-allowed' : 'pointer'}}>
                {noteSaving ? 'Saving…' : 'Save Note'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
