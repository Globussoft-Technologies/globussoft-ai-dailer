import React, { useState, useEffect, useRef } from 'react';
import { INDIAN_VOICES, INDIAN_LANGUAGES } from '../constants/voices';

const STEPS = [
  { key: 'leads', label: 'Upload Leads' },
  { key: 'voice', label: 'Configure Voice' },
  { key: 'campaign', label: 'Create Campaign' },
];

export default function OnboardingWizard({ apiFetch, API_URL, selectedOrg, orgProducts, fetchOrgProducts, onComplete }) {
  const [currentStep, setCurrentStep] = useState(0);
  const [stepStatus, setStepStatus] = useState({ leads: false, voice: false, campaign: false });
  const [allDone, setAllDone] = useState(false);

  // Step 1: CSV upload
  const [csvFile, setCsvFile] = useState(null);
  const [uploading, setUploading] = useState(false);
  const [uploadResult, setUploadResult] = useState(null);
  const fileInputRef = useRef(null);

  // Step 2: Voice
  const [voiceProvider, setVoiceProvider] = useState('sarvam');
  const [voiceId, setVoiceId] = useState('');
  const [language, setLanguage] = useState('hi');
  const [savingVoice, setSavingVoice] = useState(false);

  // Step 3: Campaign
  const [campaignName, setCampaignName] = useState('');
  const [productId, setProductId] = useState('');
  const [creatingCampaign, setCreatingCampaign] = useState(false);

  // Refresh step status
  const refreshStatus = async () => {
    try {
      const res = await apiFetch(`${API_URL}/onboarding/status`);
      const data = await res.json();
      setStepStatus(data.steps);
    } catch (e) {}
  };

  useEffect(() => { refreshStatus(); }, []);
  useEffect(() => {
    if (orgProducts && orgProducts.length > 0 && !productId) {
      setProductId(orgProducts[0].id);
    }
  }, [orgProducts]);

  // --- Step 1: CSV Upload ---
  const handleCsvUpload = async () => {
    if (!csvFile) return;
    setUploading(true);
    setUploadResult(null);
    try {
      const formData = new FormData();
      formData.append('file', csvFile);
      const res = await apiFetch(`${API_URL}/leads/import-csv`, {
        method: 'POST', body: formData
      });
      const data = await res.json();
      setUploadResult(data);
      setCsvFile(null);
      if (fileInputRef.current) fileInputRef.current.value = '';
      await refreshStatus();
    } catch (e) {
      setUploadResult({ status: 'error', errors: ['Upload failed'] });
    }
    setUploading(false);
  };

  // --- Step 2: Save Voice ---
  const handleSaveVoice = async () => {
    if (!voiceId) return;
    setSavingVoice(true);
    try {
      await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/voice-settings`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tts_provider: voiceProvider, tts_voice_id: voiceId, tts_language: language })
      });
      await refreshStatus();
    } catch (e) {}
    setSavingVoice(false);
  };

  // --- Step 3: Create Campaign ---
  const handleCreateCampaign = async () => {
    if (!campaignName.trim() || !productId) return;
    setCreatingCampaign(true);
    try {
      await apiFetch(`${API_URL}/campaigns`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: campaignName.trim(), product_id: parseInt(productId) })
      });
      await refreshStatus();
    } catch (e) {}
    setCreatingCampaign(false);
  };

  // --- Finish ---
  const handleFinish = async () => {
    try {
      await apiFetch(`${API_URL}/onboarding/complete`, { method: 'POST' });
    } catch (e) {}
    onComplete();
  };

  const voices = INDIAN_VOICES[voiceProvider] || [];
  const stepsDone = Object.values(stepStatus).filter(Boolean).length;

  // Determine if we should show the final screen
  useEffect(() => {
    if (stepStatus.leads && stepStatus.voice && stepStatus.campaign) {
      setAllDone(true);
    }
  }, [stepStatus]);

  const renderStepContent = () => {
    if (allDone) {
      return (
        <div style={{ textAlign: 'center', padding: '2rem 0' }}>
          <div style={{ fontSize: '3rem', marginBottom: '1rem' }}>&#10003;</div>
          <h2 style={{ margin: '0 0 0.5rem', color: '#e2e8f0' }}>You're all set!</h2>
          <p style={{ color: '#94a3b8', marginBottom: '2rem' }}>
            Your AI dialer is ready to make calls. Head to the dashboard to start your first campaign.
          </p>
          <button className="btn-primary" onClick={handleFinish} style={{ padding: '12px 32px', fontSize: '1rem' }}>
            Go to Dashboard
          </button>
        </div>
      );
    }

    const step = STEPS[currentStep];

    if (step.key === 'leads') {
      return (
        <div>
          <h3 style={{ margin: '0 0 0.5rem', color: '#e2e8f0' }}>Upload your first leads</h3>
          <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem' }}>
            Upload a CSV file with your leads to get started. The CSV should have columns: first_name, phone (required), last_name, source (optional).
          </p>

          {stepStatus.leads ? (
            <div style={{ background: 'rgba(16, 185, 129, 0.1)', border: '1px solid rgba(16, 185, 129, 0.3)', borderRadius: '8px', padding: '12px 16px', color: '#34d399', marginBottom: '1rem' }}>
              Leads uploaded successfully!
            </div>
          ) : (
            <>
              <div style={{ display: 'flex', gap: '12px', alignItems: 'center', marginBottom: '1rem' }}>
                <input
                  ref={fileInputRef}
                  type="file" accept=".csv"
                  onChange={e => setCsvFile(e.target.files[0])}
                  style={{ flex: 1, color: '#cbd5e1' }}
                />
                <button
                  className="btn-primary"
                  onClick={handleCsvUpload}
                  disabled={!csvFile || uploading}
                  style={{ opacity: (!csvFile || uploading) ? 0.5 : 1 }}
                >
                  {uploading ? 'Uploading...' : 'Upload CSV'}
                </button>
              </div>

              <a
                href={`${API_URL}/leads/sample-csv`}
                download
                style={{ color: '#818cf8', fontSize: '0.85rem', textDecoration: 'underline' }}
              >
                Download sample CSV
              </a>

              {uploadResult && (
                <div style={{
                  marginTop: '1rem', padding: '12px 16px', borderRadius: '8px',
                  background: uploadResult.imported > 0 ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
                  border: `1px solid ${uploadResult.imported > 0 ? 'rgba(16, 185, 129, 0.3)' : 'rgba(239, 68, 68, 0.3)'}`,
                  color: uploadResult.imported > 0 ? '#34d399' : '#f87171',
                  fontSize: '0.9rem'
                }}>
                  {uploadResult.imported > 0
                    ? `Imported ${uploadResult.imported} leads successfully!`
                    : `Import failed. ${uploadResult.errors?.join(', ') || ''}`
                  }
                </div>
              )}
            </>
          )}
        </div>
      );
    }

    if (step.key === 'voice') {
      return (
        <div>
          <h3 style={{ margin: '0 0 0.5rem', color: '#e2e8f0' }}>Choose your AI agent's voice</h3>
          <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem' }}>
            Select the language, provider, and voice for your AI calling agent.
          </p>

          {stepStatus.voice ? (
            <div style={{ background: 'rgba(16, 185, 129, 0.1)', border: '1px solid rgba(16, 185, 129, 0.3)', borderRadius: '8px', padding: '12px 16px', color: '#34d399', marginBottom: '1rem' }}>
              Voice configured successfully!
            </div>
          ) : (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '12px', marginBottom: '1rem' }}>
                <div>
                  <label style={{ display: 'block', color: '#cbd5e1', fontSize: '0.85rem', marginBottom: '6px' }}>Language</label>
                  <select className="form-input" value={language} onChange={e => setLanguage(e.target.value)}>
                    {INDIAN_LANGUAGES.map(l => <option key={l.code} value={l.code}>{l.name}</option>)}
                  </select>
                </div>
                <div>
                  <label style={{ display: 'block', color: '#cbd5e1', fontSize: '0.85rem', marginBottom: '6px' }}>Provider</label>
                  <select className="form-input" value={voiceProvider} onChange={e => { setVoiceProvider(e.target.value); setVoiceId(''); }}>
                    <option value="sarvam">Sarvam AI</option>
                    <option value="elevenlabs">ElevenLabs</option>
                    <option value="smallest">SmallestAI</option>
                  </select>
                </div>
                <div>
                  <label style={{ display: 'block', color: '#cbd5e1', fontSize: '0.85rem', marginBottom: '6px' }}>Voice</label>
                  <select className="form-input" value={voiceId} onChange={e => setVoiceId(e.target.value)}>
                    <option value="">Select voice...</option>
                    {voices.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
                  </select>
                </div>
              </div>

              <button
                className="btn-primary"
                onClick={handleSaveVoice}
                disabled={!voiceId || savingVoice}
                style={{ opacity: (!voiceId || savingVoice) ? 0.5 : 1 }}
              >
                {savingVoice ? 'Saving...' : 'Save Voice Settings'}
              </button>
            </>
          )}
        </div>
      );
    }

    if (step.key === 'campaign') {
      const hasProducts = orgProducts && orgProducts.length > 0;

      return (
        <div>
          <h3 style={{ margin: '0 0 0.5rem', color: '#e2e8f0' }}>Create your first campaign</h3>
          <p style={{ color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem' }}>
            A campaign groups your leads and lets you start bulk AI-powered calls.
          </p>

          {stepStatus.campaign ? (
            <div style={{ background: 'rgba(16, 185, 129, 0.1)', border: '1px solid rgba(16, 185, 129, 0.3)', borderRadius: '8px', padding: '12px 16px', color: '#34d399', marginBottom: '1rem' }}>
              Campaign created successfully!
            </div>
          ) : !hasProducts ? (
            <div style={{ background: 'rgba(250, 204, 21, 0.1)', border: '1px solid rgba(250, 204, 21, 0.3)', borderRadius: '8px', padding: '12px 16px', color: '#fbbf24', fontSize: '0.9rem' }}>
              You need to create a product first. Go to Settings after onboarding to add a product.
            </div>
          ) : (
            <>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px', marginBottom: '1rem' }}>
                <div>
                  <label style={{ display: 'block', color: '#cbd5e1', fontSize: '0.85rem', marginBottom: '6px' }}>Campaign Name</label>
                  <input
                    className="form-input"
                    value={campaignName}
                    onChange={e => setCampaignName(e.target.value)}
                    placeholder="e.g. April Outreach"
                  />
                </div>
                <div>
                  <label style={{ display: 'block', color: '#cbd5e1', fontSize: '0.85rem', marginBottom: '6px' }}>Product</label>
                  <select className="form-input" value={productId} onChange={e => setProductId(e.target.value)}>
                    {orgProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                </div>
              </div>

              <button
                className="btn-primary"
                onClick={handleCreateCampaign}
                disabled={!campaignName.trim() || !productId || creatingCampaign}
                style={{ opacity: (!campaignName.trim() || !productId || creatingCampaign) ? 0.5 : 1 }}
              >
                {creatingCampaign ? 'Creating...' : 'Create Campaign'}
              </button>
            </>
          )}
        </div>
      );
    }

    return null;
  };

  return (
    <div className="modal-overlay" style={{ zIndex: 100 }}>
      <div className="glass-panel" style={{ maxWidth: '600px', width: '100%', animation: 'slideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1) forwards' }}>
        {/* Header */}
        <div style={{ textAlign: 'center', marginBottom: '1.5rem' }}>
          <h2 style={{
            margin: '0 0 0.25rem',
            background: 'linear-gradient(to right, #818cf8, #d8b4fe)',
            WebkitBackgroundClip: 'text',
            WebkitTextFillColor: 'transparent',
            fontSize: '1.5rem'
          }}>
            Welcome to Callified AI
          </h2>
          <p style={{ color: '#94a3b8', margin: 0, fontSize: '0.9rem' }}>
            Let's get you set up in a few quick steps
          </p>
        </div>

        {/* Progress indicator */}
        {!allDone && (
          <div style={{ display: 'flex', gap: '8px', marginBottom: '1.5rem' }}>
            {STEPS.map((s, i) => {
              const isDone = stepStatus[s.key];
              const isActive = i === currentStep;
              return (
                <button
                  key={s.key}
                  onClick={() => setCurrentStep(i)}
                  style={{
                    flex: 1, padding: '10px 8px',
                    background: isDone ? 'rgba(16, 185, 129, 0.15)' : isActive ? 'rgba(99, 102, 241, 0.15)' : 'rgba(255,255,255,0.03)',
                    border: `1px solid ${isDone ? 'rgba(16, 185, 129, 0.3)' : isActive ? 'rgba(99, 102, 241, 0.4)' : 'rgba(255,255,255,0.08)'}`,
                    borderRadius: '8px', cursor: 'pointer',
                    color: isDone ? '#34d399' : isActive ? '#a5b4fc' : '#64748b',
                    fontSize: '0.8rem', fontWeight: 600,
                    transition: 'all 0.2s'
                  }}
                >
                  <span style={{ display: 'block', fontSize: '0.7rem', opacity: 0.7, marginBottom: '2px' }}>Step {i + 1}</span>
                  {isDone ? '\u2713 ' : ''}{s.label}
                </button>
              );
            })}
          </div>
        )}

        {/* Step content */}
        <div style={{ minHeight: '180px' }}>
          {renderStepContent()}
        </div>

        {/* Footer navigation */}
        {!allDone && (
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '1.5rem', paddingTop: '1rem', borderTop: '1px solid rgba(255,255,255,0.06)' }}>
            <button
              onClick={() => setCurrentStep(Math.max(0, currentStep - 1))}
              disabled={currentStep === 0}
              style={{
                background: 'transparent', border: '1px solid rgba(255,255,255,0.1)',
                color: currentStep === 0 ? '#475569' : '#cbd5e1',
                padding: '8px 18px', borderRadius: '8px', cursor: currentStep === 0 ? 'default' : 'pointer'
              }}
            >
              Back
            </button>

            <button
              onClick={handleFinish}
              style={{
                background: 'transparent', border: 'none',
                color: '#64748b', cursor: 'pointer', fontSize: '0.85rem',
                textDecoration: 'underline'
              }}
            >
              Skip setup
            </button>

            {currentStep < STEPS.length - 1 ? (
              <button
                onClick={() => setCurrentStep(currentStep + 1)}
                style={{
                  background: 'rgba(99, 102, 241, 0.15)', border: '1px solid rgba(99, 102, 241, 0.3)',
                  color: '#a5b4fc', padding: '8px 18px', borderRadius: '8px', cursor: 'pointer', fontWeight: 600
                }}
              >
                Next
              </button>
            ) : (
              <button
                className="btn-primary"
                onClick={() => setAllDone(true)}
              >
                Finish
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
