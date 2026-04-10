import React, { useState, useEffect } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
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
import BillingPage from './pages/BillingPage';
import ScheduledCallsPage from './pages/ScheduledCallsPage';
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
        userRole={userRole} currentUser={currentUser}
        handleLogout={logout}
      />

      <Routes>
        <Route path="/" element={<Navigate to="/crm" replace />} />
        <Route path="/crm" element={
          <CrmPage
            apiFetch={apiFetch} API_URL={API_URL}
            selectedOrg={selectedOrg} orgTimezone={orgTimezone}
            dialingId={dialingId} setDialingId={setDialingId}
            webCallActive={webCallActive}
            handleDial={handleDial} handleWebCall={handleWebCall}
            campaigns={campaigns}
            activeVoiceProvider={activeVoiceProvider} setActiveVoiceProvider={setActiveVoiceProvider}
            activeVoiceId={activeVoiceId} setActiveVoiceId={setActiveVoiceId}
            activeLanguage={activeLanguage} setActiveLanguage={setActiveLanguage}
            INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
            savedVoiceName={savedVoiceName} setSavedVoiceName={setSavedVoiceName}
            userRole={userRole} authToken={authToken}
          />
        } />
        <Route path="/campaigns" element={
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
        } />
        <Route path="/ops" element={<OpsPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="/analytics" element={<AnalyticsPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="/whatsapp" element={<WhatsAppPage apiFetch={apiFetch} API_URL={API_URL} orgProducts={orgProducts} selectedOrg={selectedOrg} orgTimezone={orgTimezone} />} />
        <Route path="/integrations" element={<IntegrationsPage apiFetch={apiFetch} API_URL={API_URL} orgTimezone={orgTimezone} />} />
        <Route path="/monitor" element={<MonitorPage API_URL={API_URL} />} />
        <Route path="/knowledge" element={<KnowledgePage API_URL={API_URL} />} />
        <Route path="/sandbox" element={<SandboxPage API_URL={API_URL} />} />
        <Route path="/settings" element={
          <SettingsPage
            apiFetch={apiFetch} API_URL={API_URL}
            selectedOrg={selectedOrg} orgs={orgs}
            orgProducts={orgProducts} orgTimezone={orgTimezone}
            fetchOrgProducts={fetchOrgProducts}
          />
        } />
        <Route path="/logs" element={<LogsPage API_URL={API_URL} authToken={authToken} />} />
        <Route path="/checkin" element={<CheckInPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="/billing" element={<BillingPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="/scheduled" element={<ScheduledCallsPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="*" element={<Navigate to="/crm" replace />} />
      </Routes>

    </div>
  );
}
