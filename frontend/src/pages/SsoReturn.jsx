import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

// SsoReturn handles the redirect from /api/auth/sso/jwt. The backend has
// already verified the inbound JWT, minted our internal JWT, and bounced
// here with ?token=<our-jwt>&next=<original redirect>. We store it, fetch
// the user profile, then navigate the SPA to ?next=. If anything fails the
// backend forwards us with ?error=<code> instead — we render that as a
// short, plain message rather than a stack trace.
//
// Public route — must be reachable without an existing session, so it sits
// before App.jsx's authToken gate.
export default function SsoReturn() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { loginWithToken } = useAuth();
  const [error, setError] = useState('');

  useEffect(() => {
    const token = params.get('token');
    const next = params.get('next') || '/crm';
    const errParam = params.get('error');

    if (errParam) {
      setError(errParam);
      return;
    }
    if (!token) {
      setError('missing_token');
      return;
    }
    loginWithToken(token)
      .then(() => navigate(next, { replace: true }))
      .catch((e) => setError(String(e.message || 'sso_handshake_failed')));
  }, []); // run once on mount

  return (
    <div style={{ padding: '3rem', textAlign: 'center', fontFamily: 'system-ui' }}>
      {error ? (
        <>
          <h2 style={{ color: '#ef4444' }}>SSO sign-in failed</h2>
          <p style={{ color: '#94a3b8' }}>code: {error}</p>
          <p style={{ marginTop: '1.5rem' }}>
            <a href="/" style={{ color: '#60a5fa' }}>Back to login</a>
          </p>
        </>
      ) : (
        <p style={{ color: '#94a3b8' }}>Signing you in…</p>
      )}
    </div>
  );
}
