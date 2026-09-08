import React, { useState } from 'react';
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
  // Transcript state (for campaign lead transcripts)
  const [transcriptLead, setTranscriptLead] = useState(null);
  const [transcripts, setTranscripts] = useState([]);

  const handleViewTranscripts = async (lead) => {
    setTranscriptLead(lead);
    try {
      const query = lead.campaign_id ? `?campaign_id=${encodeURIComponent(lead.campaign_id)}` : '';
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/transcripts${query}`);
      if (!res.ok) { setTranscripts([]); return; }
      const data = await res.json();
      setTranscripts(Array.isArray(data) ? data : []);
    } catch { setTranscripts([]);  }
  };

  return (
    <>
      <CampaignsTab
        campaigns={campaigns} fetchCampaigns={fetchCampaigns}
        orgProducts={orgProducts}
        apiFetch={apiFetch} API_URL={API_URL} selectedOrg={selectedOrg}
        onCampaignDial={handleCampaignDial} onCampaignWebCall={handleCampaignWebCall}
        activeVoiceProvider={activeVoiceProvider} activeVoiceId={activeVoiceId}
        activeLanguage={activeLanguage}
        INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
        dialingId={dialingId} webCallActive={webCallActive}
        handleViewTranscripts={handleViewTranscripts}
        orgTimezone={orgTimezone}
      />

      <TranscriptModal
        transcriptLead={transcriptLead} setTranscriptLead={setTranscriptLead}
        transcripts={transcripts} orgTimezone={orgTimezone}
        onRefresh={handleViewTranscripts}
      />

    </>
  );
}
