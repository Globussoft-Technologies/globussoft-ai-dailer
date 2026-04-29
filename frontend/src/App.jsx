import React, { useState, useEffect } from 'react';
import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import ResetPasswordPage from './pages/ResetPasswordPage';
import SsoReturn from './pages/SsoReturn';
import MonitorPage from './pages/MonitorPage';
import KnowledgePage from './pages/KnowledgePage';
import SandboxPage from './pages/SandboxPage';
import AuthPage from './components/AuthPage';
import TopHeader from './components/TopHeader';
import OnboardingWizard from './components/OnboardingWizard';
import RequireRole from './components/RequireRole';
import CrmPage from './pages/CrmPage';
import OpsPage from './pages/OpsPage';
import AnalyticsPage from './pages/AnalyticsPage';
import WhatsAppPage from './pages/WhatsAppPage';
import IntegrationsPage from './pages/IntegrationsPage';
import SettingsPage from './pages/SettingsPage';
import LogsPage from './pages/LogsPage';
import CheckInPage from './pages/CheckInPage';
import BillingPage from './pages/BillingPage';
import DndPage from './pages/DndPage';
import ScheduledCallsPage from './pages/ScheduledCallsPage';
import CampaignsPage from './pages/CampaignsPage';
import TeamPage from './pages/TeamPage';
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
  const [showOnboarding, setShowOnboarding] = useState(false);

  const fetchCampaigns = async () => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns`);
      const data = await res.json();
      if (!Array.isArray(data)) {
        console.warn('[fetchCampaigns] expected array, got:', { status: res.status, body: data });
        setCampaigns([]);
        return;
      }
      setCampaigns(data);
    } catch(e) {
      console.warn('[fetchCampaigns] error:', e);
    }
  };

  useEffect(() => {
    if (!currentUser) return;
    fetchCampaigns();
    // Check onboarding status
    (async () => {
      try {
        const res = await apiFetch(`${API_URL}/onboarding/status`);
        const data = await res.json();
        if (!data.completed) setShowOnboarding(true);
      } catch (e) {}
    })();
  }, [currentUser]);

  // ─── PUBLIC ROUTES (no auth required) ───
  // /sso/return is hit before any session exists — the backend's SSO endpoint
  // bounces the browser here with ?token=<jwt>&next=… for the SPA to commit
  // the token and continue. Must short-circuit before the authToken gate or
  // a brand-new SSO user would loop back to AuthPage.
  const location = useLocation();
  if (location.pathname === '/reset-password') {
    return <ResetPasswordPage />;
  }
  if (location.pathname === '/sso/return') {
    return <SsoReturn />;
  }

  // ─── AUTH PAGES (after all hooks) ───
  if (!authToken || !currentUser) {
    return <AuthPage />;
  }

  return (
    <div className="dashboard-container">
      {showOnboarding && (
        <OnboardingWizard
          apiFetch={apiFetch} API_URL={API_URL}
          selectedOrg={selectedOrg}
          orgProducts={orgProducts}
          fetchOrgProducts={fetchOrgProducts}
          onComplete={() => setShowOnboarding(false)}
        />
      )}
      <TopHeader
        userRole={userRole} currentUser={currentUser}
        handleLogout={logout}
      />

      <main className="main-content">
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
        {/*
          Admin-only routes. Wrapping in <RequireRole> redirects non-Admins
          to /crm if they type the URL directly — the TopHeader already
          hides the corresponding nav buttons. Pair this with the backend's
          adminAuth middleware so a malicious client can't just call the
          API directly (defense in depth).
        */}
        <Route path="/campaigns" element={
          <RequireRole>
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
          </RequireRole>
        } />
        <Route path="/ops" element={<RequireRole><OpsPage apiFetch={apiFetch} API_URL={API_URL} /></RequireRole>} />
        <Route path="/analytics" element={<RequireRole><AnalyticsPage apiFetch={apiFetch} API_URL={API_URL} /></RequireRole>} />
        <Route path="/whatsapp" element={<RequireRole><WhatsAppPage apiFetch={apiFetch} API_URL={API_URL} orgProducts={orgProducts} selectedOrg={selectedOrg} orgTimezone={orgTimezone} /></RequireRole>} />
        <Route path="/integrations" element={<RequireRole><IntegrationsPage apiFetch={apiFetch} API_URL={API_URL} orgTimezone={orgTimezone} /></RequireRole>} />
        <Route path="/monitor" element={<RequireRole><MonitorPage API_URL={API_URL} /></RequireRole>} />
        <Route path="/knowledge" element={<RequireRole><KnowledgePage API_URL={API_URL} /></RequireRole>} />
        <Route path="/sandbox" element={<RequireRole><SandboxPage API_URL={API_URL} /></RequireRole>} />
        <Route path="/settings" element={
          <RequireRole>
            <SettingsPage
              apiFetch={apiFetch} API_URL={API_URL}
              selectedOrg={selectedOrg} orgs={orgs}
              orgProducts={orgProducts} orgTimezone={orgTimezone}
              fetchOrgProducts={fetchOrgProducts}
            />
          </RequireRole>
        } />
        <Route path="/logs" element={<RequireRole><LogsPage API_URL={API_URL} authToken={authToken} apiFetch={apiFetch} /></RequireRole>} />
        <Route path="/checkin" element={<CheckInPage apiFetch={apiFetch} API_URL={API_URL} />} />
        <Route path="/billing" element={<RequireRole><BillingPage apiFetch={apiFetch} API_URL={API_URL} /></RequireRole>} />
        <Route path="/dnd" element={<RequireRole><DndPage apiFetch={apiFetch} API_URL={API_URL} /></RequireRole>} />
        <Route path="/scheduled" element={<RequireRole><ScheduledCallsPage apiFetch={apiFetch} API_URL={API_URL} orgTimezone={orgTimezone} /></RequireRole>} />
        <Route path="/team" element={<RequireRole><TeamPage apiFetch={apiFetch} API_URL={API_URL} /></RequireRole>} />
        <Route path="*" element={<Navigate to="/crm" replace />} />
      </Routes>
      </main>

    </div>
  );
}
