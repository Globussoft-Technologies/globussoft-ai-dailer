import React, { useState } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { API_URL } from '../constants/api';

export default function ResetPasswordPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const token = searchParams.get('token');

  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError(''); setSuccess('');

    if (newPassword.length < 8) {
      setError('Password must be at least 8 characters.');
      return;
    }
    if (newPassword !== confirmPassword) {
      setError('Passwords do not match');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(`${API_URL}/auth/reset-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, new_password: newPassword }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.detail || 'Reset failed');
      setSuccess(data.message);
    } catch (err) {
      setError(err.message);
    }
    setLoading(false);
  };

  return (
    <div style={{minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'linear-gradient(135deg, #0f0c29, #302b63, #24243e)', padding: '20px'}}>
      <div style={{width: '100%', maxWidth: '440px'}}>
        <div style={{textAlign: 'center', marginBottom: '2rem'}}>
          <h1 style={{fontSize: '2rem', fontWeight: 800, background: 'linear-gradient(135deg, #a78bfa, #22d3ee)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent'}}>
            Callified AI
          </h1>
          <p style={{color: '#94a3b8', fontSize: '0.95rem'}}>Reset Your Password</p>
        </div>

        <div className="glass-panel" style={{padding: '2rem'}}>
          {!token ? (
            <div style={{textAlign: 'center'}}>
              <p style={{color: '#fca5a5', marginBottom: '1rem'}}>Invalid reset link. No token provided.</p>
              <button onClick={() => navigate('/')}
                style={{background: 'none', border: 'none', color: '#a78bfa', cursor: 'pointer', textDecoration: 'underline'}}>
                Back to Login
              </button>
            </div>
          ) : success ? (
            <div style={{textAlign: 'center'}}>
              <div style={{background: 'rgba(34,197,94,0.15)', border: '1px solid rgba(34,197,94,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#86efac', fontSize: '0.9rem'}}>
                {success}
              </div>
              <button onClick={() => navigate('/')}
                className="btn-primary"
                style={{padding: '12px 28px', fontSize: '1rem', fontWeight: 700, background: 'linear-gradient(135deg, #a78bfa, #7c3aed)'}}>
                Go to Login
              </button>
            </div>
          ) : (
            <>
              {error && (
                <div style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#fca5a5', fontSize: '0.85rem'}}>
                  {error}
                </div>
              )}
              <form onSubmit={handleSubmit}>
                <div className="form-group">
                  <label>New Password</label>
                  <input className="form-input" type="password" placeholder="Enter new password" required minLength={8} maxLength={128}
                    value={newPassword} onChange={e => setNewPassword(e.target.value)} />
                </div>
                <div className="form-group">
                  <label>Confirm Password</label>
                  <input className="form-input" type="password" placeholder="Confirm new password" required minLength={8} maxLength={128}
                    value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} />
                </div>
                <button type="submit" className="btn-primary" disabled={loading}
                  style={{width: '100%', padding: '12px', marginTop: '0.5rem', fontSize: '1rem', fontWeight: 700,
                    background: 'linear-gradient(135deg, #a78bfa, #7c3aed)'}}>
                  {loading ? 'Resetting...' : 'Reset Password'}
                </button>
              </form>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
