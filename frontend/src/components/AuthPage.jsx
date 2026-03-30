import React from 'react';

export default function AuthPage({
  authPage,
  setAuthPage,
  authError,
  setAuthError,
  authLoading,
  authForm,
  setAuthForm,
  handleLogin,
  handleSignup
}) {
  return (
    <div style={{minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'linear-gradient(135deg, #0f0c29, #302b63, #24243e)', padding: '20px'}}>
      <div style={{width: '100%', maxWidth: '440px'}}>
        <div style={{textAlign: 'center', marginBottom: '2rem'}}>
          <h1 style={{fontSize: '2rem', fontWeight: 800, background: 'linear-gradient(135deg, #a78bfa, #22d3ee)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent'}}>
            🤖 Callified AI
          </h1>
          <p style={{color: '#94a3b8', fontSize: '0.95rem'}}>AI-Powered Lead Qualification Platform</p>
          <span style={{display:'none'}} data-version="2.0.1" />
        </div>

        <div className="glass-panel" style={{padding: '2rem'}}>
          <div style={{display: 'flex', marginBottom: '1.5rem', borderRadius: '8px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)'}}>
            <button data-testid="auth-login-tab" onClick={() => { setAuthPage('login'); setAuthError(''); }}
              style={{flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                background: authPage === 'login' ? 'rgba(167,139,250,0.2)' : 'transparent',
                color: authPage === 'login' ? '#a78bfa' : '#64748b'}}>
              Login
            </button>
            <button data-testid="auth-signup-tab" onClick={() => { setAuthPage('signup'); setAuthError(''); }}
              style={{flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                background: authPage === 'signup' ? 'rgba(34,211,238,0.2)' : 'transparent',
                color: authPage === 'signup' ? '#22d3ee' : '#64748b'}}>
              Sign Up
            </button>
          </div>

          {authError && (
            <div style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#fca5a5', fontSize: '0.85rem'}}>
              {authError}
            </div>
          )}

          <form onSubmit={authPage === 'login' ? handleLogin : handleSignup}>
            {authPage === 'signup' && (
              <>
                <div className="form-group">
                  <label>Organization Name</label>
                  <input data-testid="auth-org-name" className="form-input" placeholder="e.g. Globussoft" required
                    value={authForm.org_name} onChange={e => setAuthForm({...authForm, org_name: e.target.value})} />
                </div>
                <div className="form-group">
                  <label>Your Full Name</label>
                  <input data-testid="auth-full-name" className="form-input" placeholder="e.g. Sumit Kumar" required
                    value={authForm.full_name} onChange={e => setAuthForm({...authForm, full_name: e.target.value})} />
                </div>
              </>
            )}
            <div className="form-group">
              <label>Email</label>
              <input data-testid="auth-email" className="form-input" type="email" placeholder="you@company.com" required
                value={authForm.email} onChange={e => setAuthForm({...authForm, email: e.target.value})} />
            </div>
            <div className="form-group">
              <label>Password</label>
              <input data-testid="auth-password" className="form-input" type="password" placeholder="••••••••" required minLength={6}
                value={authForm.password} onChange={e => setAuthForm({...authForm, password: e.target.value})} />
            </div>
            <button data-testid="auth-submit" type="submit" className="btn-primary" disabled={authLoading}
              style={{width: '100%', padding: '12px', marginTop: '0.5rem', fontSize: '1rem', fontWeight: 700,
                background: authPage === 'login' ? 'linear-gradient(135deg, #a78bfa, #7c3aed)' : 'linear-gradient(135deg, #22d3ee, #06b6d4)'}}>
              {authLoading ? '⏳ Please wait...' : (authPage === 'login' ? '🔐 Login' : '🚀 Create Account')}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}
