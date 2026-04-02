import React, { useState, useEffect } from 'react';
import MonitorPage from './pages/MonitorPage';
import KnowledgePage from './pages/KnowledgePage';
import SandboxPage from './pages/SandboxPage';
import AuthPage from './components/AuthPage';
import TopHeader from './components/TopHeader';
import CrmPage from './pages/CrmPage';
import OpsPage from './pages/OpsPage';
import AnalyticsPage from './pages/AnalyticsPage';
import WhatsAppPage from './pages/WhatsAppPage';
import IntegrationsPage from './pages/IntegrationsPage';
import SettingsPage from './pages/SettingsPage';
import LogsPage from './pages/LogsPage';
import CheckInPage from './pages/CheckInPage';
import CampaignsPage from './pages/CampaignsPage';
import './index.css';
import { API_URL } from './constants/api';
import { INDIAN_VOICES, INDIAN_LANGUAGES } from './constants/voices';
import { useAuth } from './contexts/AuthContext';
import { useOrg } from './contexts/OrgContext';
import { useVoice } from './contexts/VoiceContext';
import { useCall } from './contexts/CallContext';

export default function App() {
  const { authToken, currentUser, apiFetch, logout } = useAuth();
  const { selectedOrg, orgTimezone, orgProducts, orgs, fetchOrgProducts } = useOrg();
  const { activeVoiceProvider, setActiveVoiceProvider, activeVoiceId, setActiveVoiceId, activeLanguage, setActiveLanguage, savedVoiceName, setSavedVoiceName } = useVoice();
  const { dialingId, setDialingId, webCallActive, handleDial, handleWebCall, handleCampaignDial, handleCampaignWebCall } = useCall();

  const [activeTab, setActiveTab] = useState('crm');

  // RBAC Global State
  const userRole = currentUser?.role || 'Agent';

  const [campaigns, setCampaigns] = useState([]);

  const fetchCampaigns = async () => {
    try { const res = await apiFetch(`${API_URL}/campaigns`); setCampaigns(await res.json()); } catch(e){}
  };

  useEffect(() => {
    if (!currentUser) return;
    fetchCampaigns();
  }, [currentUser]);

  // ─── AUTH PAGES (after all hooks) ───
  if (!authToken || !currentUser) {
    return <AuthPage />;
  }

  return (
    <div className="dashboard-container">
      <TopHeader
        activeTab={activeTab} setActiveTab={setActiveTab}
        userRole={userRole} currentUser={currentUser}
        handleLogout={logout}
      />

      {activeTab === 'crm' ? (
        <CrmPage
          apiFetch={apiFetch} API_URL={API_URL}
          selectedOrg={selectedOrg} orgTimezone={orgTimezone}
          dialingId={dialingId} setDialingId={setDialingId}
          webCallActive={webCallActive}
          handleDial={handleDial} handleWebCall={handleWebCall}
          campaigns={campaigns}
          onCampaignClick={(campaign) => { setActiveTab('campaigns'); }}
          activeVoiceProvider={activeVoiceProvider} setActiveVoiceProvider={setActiveVoiceProvider}
          activeVoiceId={activeVoiceId} setActiveVoiceId={setActiveVoiceId}
          activeLanguage={activeLanguage} setActiveLanguage={setActiveLanguage}
          INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
          savedVoiceName={savedVoiceName} setSavedVoiceName={setSavedVoiceName}
          userRole={userRole} authToken={authToken}
        />
      ) : activeTab === 'campaigns' ? (
        <CampaignsPage
          apiFetch={apiFetch} API_URL={API_URL}
          selectedOrg={selectedOrg} orgTimezone={orgTimezone} orgProducts={orgProducts}
          dialingId={dialingId} webCallActive={webCallActive}
          handleCampaignDial={handleCampaignDial} handleCampaignWebCall={handleCampaignWebCall}
          activeVoiceProvider={activeVoiceProvider} activeVoiceId={activeVoiceId}
          activeLanguage={activeLanguage}
          INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
          campaigns={campaigns} fetchCampaigns={fetchCampaigns}
        />
      ) : activeTab === 'ops' ? (
        <OpsPage apiFetch={apiFetch} API_URL={API_URL} />
      ) : activeTab === 'analytics' ? (
        <AnalyticsPage apiFetch={apiFetch} API_URL={API_URL} />
      ) : activeTab === 'whatsapp' ? (
        <WhatsAppPage apiFetch={apiFetch} API_URL={API_URL} orgProducts={orgProducts} selectedOrg={selectedOrg} orgTimezone={orgTimezone} />
      ) : activeTab === 'integrations' ? (
        <IntegrationsPage apiFetch={apiFetch} API_URL={API_URL} orgTimezone={orgTimezone} />
      ) : activeTab === 'monitor' ? (
        <MonitorPage API_URL={API_URL} />
      ) : activeTab === 'knowledge' ? (
        <KnowledgePage API_URL={API_URL} />
      ) : activeTab === 'sandbox' ? (
        <SandboxPage API_URL={API_URL} />
      ) : activeTab === 'settings' ? (
        <SettingsPage
          apiFetch={apiFetch} API_URL={API_URL}
          selectedOrg={selectedOrg} orgs={orgs}
          orgProducts={orgProducts} orgTimezone={orgTimezone}
          fetchOrgProducts={fetchOrgProducts}
        />
      ) : activeTab === 'logs' ? (
        <LogsPage API_URL={API_URL} authToken={authToken} />
      ) : (
        <CheckInPage apiFetch={apiFetch} API_URL={API_URL} />
      )}

    </div>
  );
}
