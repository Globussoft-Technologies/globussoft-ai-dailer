import React, { useState } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { API_URL } from '../constants/api';

export default function AuthPage() {
  const { login, signup } = useAuth();

  const [authPage, setAuthPage] = useState('login');
  const [authError, setAuthError] = useState('');
  const [authLoading, setAuthLoading] = useState(false);
  const [authForm, setAuthForm] = useState({ org_name: '', full_name: '', email: '', password: '' });
  const [forgotEmail, setForgotEmail] = useState('');
  const [forgotSuccess, setForgotSuccess] = useState('');

  const handleLogin = async (e) => {
    e.preventDefault();
    setAuthError(''); setAuthLoading(true);
    try {
      await login(authForm.email, authForm.password);
    } catch (err) { setAuthError(err.message); }
    setAuthLoading(false);
  };


  const handleSignup = async (e) => {
    e.preventDefault();
    setAuthError(''); setAuthLoading(true);
    try {
      await signup(authForm.org_name, authForm.full_name, authForm.email, authForm.password);
    } catch (err) { setAuthError(err.message); }
    setAuthLoading(false);
  };

  const handleForgotPassword = async (e) => {
    e.preventDefault();
    setAuthError(''); setForgotSuccess(''); setAuthLoading(true);
    try {
      const res = await fetch(`${API_URL}/auth/forgot-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: forgotEmail }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.detail || 'Request failed');
      setForgotSuccess(data.message);
    } catch (err) { setAuthError(err.message); }
    setAuthLoading(false);
  };

  return (
    <div style={{
      minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'linear-gradient(135deg, #0f0c29, #302b63, #24243e)', padding: '20px'
    }}>
      <div style={{ width: '100%', maxWidth: '440px' }}>
        <div style={{ textAlign: 'center', marginBottom: '2rem' }}>
          <img src="/login_logo.png" alt="Callified AI" style={{ maxWidth: '240px', width: '100%', height: 'auto' }} />
          <span style={{ display: 'none' }} data-version="2.0.1" />
        </div>

        <div className="glass-panel" style={{ padding: '2rem' }}>
          {authPage !== 'forgot' && (
            <div style={{ display: 'flex', marginBottom: '1.5rem', borderRadius: '8px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)' }}>
              <button data-testid="auth-login-tab" onClick={() => { setAuthPage('login'); setAuthError(''); setForgotSuccess(''); }}
                style={{
                  flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                  background: authPage === 'login' ? 'rgba(167,139,250,0.2)' : 'transparent',
                  color: authPage === 'login' ? '#a78bfa' : '#64748b'
                }}>
                Login
              </button>
              <button data-testid="auth-signup-tab" onClick={() => { setAuthPage('signup'); setAuthError(''); setForgotSuccess(''); }}
                style={{
                  flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                  background: authPage === 'signup' ? 'rgba(34,211,238,0.2)' : 'transparent',
                  color: authPage === 'signup' ? '#22d3ee' : '#64748b'
                }}>
                Sign Up
              </button>
            </div>
          )}

          {authPage === 'forgot' && (
            <div style={{ marginBottom: '1.5rem' }}>
              <button onClick={() => { setAuthPage('login'); setAuthError(''); setForgotSuccess(''); }}
                style={{ background: 'none', border: 'none', color: '#a78bfa', cursor: 'pointer', fontSize: '0.9rem', padding: 0 }}>
                &larr; Back to Login
              </button>
              <h2 style={{ color: '#f8fafc', fontSize: '1.2rem', marginTop: '0.75rem', marginBottom: 0 }}>Forgot Password</h2>
            </div>
          )}

          {authError && (
            <div style={{ background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#fca5a5', fontSize: '0.85rem' }}>
              {authError}
            </div>
          )}

          {forgotSuccess && (
            <div style={{ background: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#86efac', fontSize: '0.85rem' }}>
              {forgotSuccess}
            </div>
          )}

          {authPage === 'forgot' ? (
            <form onSubmit={handleForgotPassword}>
              <div className="form-group">
                <label>Email Address</label>
                <input className="form-input" type="email" placeholder="you@company.com" required
                  value={forgotEmail} onChange={e => setForgotEmail(e.target.value)} />
              </div>
              <button type="submit" className="btn-primary" disabled={authLoading}
                style={{
                  width: '100%', padding: '12px', marginTop: '0.5rem', fontSize: '1rem', fontWeight: 700,
                  background: 'linear-gradient(135deg, #a78bfa, #7c3aed)'
                }}>
                {authLoading ? 'Sending...' : 'Send Reset Link'}
              </button>
            </form>
          ) : (
            <>
              <form onSubmit={authPage === 'login' ? handleLogin : handleSignup}>
                {authPage === 'signup' && (
                  <>
                    <div className="form-group">
                      <label>Organization Name</label>
                      <input data-testid="auth-org-name" className="form-input" placeholder="e.g. Globussoft" required
                        value={authForm.org_name} onChange={e => setAuthForm({ ...authForm, org_name: e.target.value })} />
                    </div>
                    <div className="form-group">
                      <label>Your Full Name</label>
                      <input data-testid="auth-full-name" className="form-input" placeholder="e.g. Sumit Kumar" required
                        value={authForm.full_name} onChange={e => setAuthForm({ ...authForm, full_name: e.target.value })} />
                    </div>
                  </>
                )}
                <div className="form-group">
                  <label>Email</label>
                  <input data-testid="auth-email" className="form-input" type="email" placeholder="you@company.com" required
                    value={authForm.email} onChange={e => setAuthForm({ ...authForm, email: e.target.value })} />
                </div>
                <div className="form-group">
                  <label>Password</label>
                  <input data-testid="auth-password" className="form-input" type="password" placeholder="••••••••" required minLength={6}
                    value={authForm.password} onChange={e => setAuthForm({ ...authForm, password: e.target.value })} />
                </div>
                <button data-testid="auth-submit" type="submit" className="btn-primary" disabled={authLoading}
                  style={{
                    width: '100%', padding: '12px', marginTop: '0.5rem', fontSize: '1rem', fontWeight: 700,
                    background: authPage === 'login' ? 'linear-gradient(135deg, #a78bfa, #7c3aed)' : 'linear-gradient(135deg, #22d3ee, #06b6d4)'
                  }}>
                  {authLoading ? '⏳ Please wait...' : (authPage === 'login' ? '🔐 Login' : '🚀 Create Account')}
                </button>
              </form>
              {authPage === 'login' && (
                <div style={{ textAlign: 'center', marginTop: '1rem' }}>
                  <button onClick={() => { setAuthPage('forgot'); setAuthError(''); setForgotSuccess(''); setForgotEmail(''); }}
                    style={{ background: 'none', border: 'none', color: '#94a3b8', cursor: 'pointer', fontSize: '0.85rem', textDecoration: 'underline' }}>
                    Forgot password?
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
