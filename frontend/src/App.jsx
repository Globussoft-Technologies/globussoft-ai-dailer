import React from 'react';
import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import ResetPasswordPage from './pages/ResetPasswordPage';
import AcceptInvitePage from './pages/AcceptInvitePage';
import SsoReturn from './pages/SsoReturn';
import AuthPage from './components/AuthPage';
import TopHeader from './components/TopHeader';
import OnboardingWizard from './components/OnboardingWizard';
import ProtectedRoute from './components/ProtectedRoute';
import { routes } from './utils/routeConfig';
import './index.css';
import { API_URL } from './constants/api';
import { INDIAN_VOICES, INDIAN_LANGUAGES } from './constants/voices';
import { useAuth } from './contexts/AuthContext';
import { useOrg } from './contexts/OrgContext';
import { useVoice } from './contexts/VoiceContext';
import { useCall } from './contexts/CallContext';
import { useCampaigns } from './hooks/useQueries';
import { queryClient } from './queryClient';

export default function App() {
  const { authToken, currentUser, apiFetch, logout, loading } = useAuth();
  const { selectedOrg, orgTimezone, orgProducts, orgs, fetchOrgProducts } = useOrg();
  const { activeVoiceProvider, setActiveVoiceProvider, activeVoiceId, setActiveVoiceId, activeLanguage, setActiveLanguage, savedVoiceName, setSavedVoiceName } = useVoice();
  const { dialingId, setDialingId, webCallActive, handleDial, handleWebCall, handleCampaignDial, handleCampaignWebCall } = useCall();

  const location = useLocation();
  const userRole = currentUser?.role || 'Agent';

  const { data: campaigns = [], isLoading: campaignsLoading } = useCampaigns();

  const { data: onboardingStatus } = useQuery({
    queryKey: ['onboarding', 'status'],
    queryFn: async () => {
      const res = await apiFetch(`${API_URL}/onboarding/status`);
      if (!res.ok) return { completed: true };
      return res.json();
    },
    enabled: Boolean(currentUser),
  });
  const showOnboarding = onboardingStatus ? !onboardingStatus.completed : false;

  const fetchCampaigns = () => {
    queryClient.invalidateQueries({ queryKey: ['campaigns'] });
  };

  // ─── PUBLIC ROUTES (no auth required) ───
  if (location.pathname === '/reset-password') {
    return <ResetPasswordPage />;
  }
  if (location.pathname === '/accept-invite') {
    return <AcceptInvitePage />;
  }
  if (location.pathname === '/sso/return') {
    return <SsoReturn />;
  }

  // ─── AUTH PAGES (after all hooks) ───
  if (loading) {
    return (
      <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'linear-gradient(135deg, #0f0c29, #302b63, #24243e)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '1rem' }}>
          <div style={{ width: 40, height: 40, border: '3px solid rgba(255,255,255,0.1)', borderTop: '3px solid #a78bfa', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
          <span style={{ color: '#94a3b8', fontSize: '0.9rem' }}>Loading...</span>
        </div>
        <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
      </div>
    );
  }

  if (!currentUser) {
    return <AuthPage redirectTo={location.pathname !== '/reset-password' ? location.pathname : '/crm'} />;
  }

  const commonProps = {
    apiFetch,
    API_URL,
    selectedOrg,
    orgTimezone,
    orgProducts,
    orgs,
    fetchOrgProducts,
    dialingId,
    setDialingId,
    webCallActive,
    handleDial,
    handleWebCall,
    handleCampaignDial,
    handleCampaignWebCall,
    activeVoiceProvider,
    setActiveVoiceProvider,
    activeVoiceId,
    setActiveVoiceId,
    activeLanguage,
    setActiveLanguage,
    savedVoiceName,
    setSavedVoiceName,
    INDIAN_VOICES,
    INDIAN_LANGUAGES,
    userRole,
    authToken,
    currentUser,
    campaigns,
    campaignsLoading,
    fetchCampaigns,
  };

  return (
    <div className="dashboard-container">
      {showOnboarding && (
        <OnboardingWizard
          apiFetch={apiFetch} API_URL={API_URL}
          selectedOrg={selectedOrg}
          orgProducts={orgProducts}
          fetchOrgProducts={fetchOrgProducts}
          onComplete={() => queryClient.setQueryData(['onboarding', 'status'], { completed: true })}
        />
      )}
      <TopHeader
        userRole={userRole} currentUser={currentUser}
        handleLogout={logout}
        apiFetch={apiFetch}
      />

      <main className="main-content">
        <Routes>
          <Route path="/" element={<Navigate to="/crm" replace />} />
          {routes.map((route) => (
            <Route
              key={route.path}
              path={route.path}
              element={<ProtectedRoute route={route} {...commonProps} />}
            />
          ))}
          <Route path="/receptionist" element={<Navigate to="/ai-receptionist" replace />} />
          <Route path="/rag" element={<Navigate to="/knowledge" replace />} />
          <Route path="/livelogs" element={<Navigate to="/logs" replace />} />
          <Route path="*" element={<Navigate to="/crm" replace />} />
        </Routes>
      </main>
    </div>
  );
}
