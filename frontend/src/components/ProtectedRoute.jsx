import React, { Suspense } from 'react';
import { Navigate, useLocation } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

function PageSpinner() {
  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div style={{ width: 40, height: 40, border: '3px solid rgba(99,102,241,0.2)', borderTop: '3px solid #6366f1', borderRadius: '50%', animation: 'spin 0.8s linear infinite' }} />
      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}

export default function ProtectedRoute({ route, ...pageProps }) {
  const { currentUser, hasPermission, hideAiFeatures } = useAuth();
  const location = useLocation();
  const userRole = currentUser?.role;
  const isSuperAdmin = currentUser?.is_super_admin === true;

  const { roles = ['Admin', 'SuperAdmin', 'TeamLeader', 'Agent', 'Executive'], aiFeatures, permission, element: Page } = route;

  const allowedByRole = roles.includes(userRole) || (roles.includes('SuperAdmin') && isSuperAdmin);
  if (!allowedByRole) {
    return <Navigate to="/crm" replace state={{ from: location }} />;
  }

  if (permission && !hasPermission(permission)) {
    return <Navigate to="/crm" replace state={{ from: location }} />;
  }

  if (aiFeatures && hideAiFeatures) {
    return <Navigate to="/crm" replace />;
  }

  return (
    <Suspense fallback={<PageSpinner />}>
      <Page {...pageProps} />
    </Suspense>
  );
}
