import React, { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';

export default function TeamPage({ apiFetch, API_URL }) {
  const { currentUser } = useAuth();
  const [members, setMembers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showInvite, setShowInvite] = useState(false);
  const [inviteForm, setInviteForm] = useState({ email: '', full_name: '', role: 'Agent', password: '' });
  const [inviteError, setInviteError] = useState('');
  const [inviteLoading, setInviteLoading] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(null);

  useEffect(() => { fetchTeam(); }, []);

  const fetchTeam = async () => {
    setLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/team`);
      if (res.ok) setMembers(await res.json());
    } catch (e) { console.error('Team fetch error:', e); }
    setLoading(false);
  };

  const closeInvite = () => {
    setShowInvite(false);
    setInviteForm({ email: '', full_name: '', role: 'Agent', password: '' });
    setInviteError('');
  };

  const handleInvite = async (e) => {
    e.preventDefault();
    setInviteError('');
    setInviteLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/team/invite`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(inviteForm),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        closeInvite();
        fetchTeam();
      } else {
        setInviteError(data.error || data.detail || 'Failed to invite user');
      }
    } catch (e) {
      setInviteError('Network error');
    }
    setInviteLoading(false);
  };

  const handleRoleChange = async (userId, newRole) => {
    try {
      const res = await apiFetch(`${API_URL}/team/${userId}/role`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ role: newRole }),
      });
      if (res.ok) fetchTeam();
      else {
        const data = await res.json();
        alert(data.detail || 'Failed to update role');
      }
    } catch (e) { alert('Network error'); }
  };

  const handleDelete = async (userId) => {
    try {
      const res = await apiFetch(`${API_URL}/team/${userId}`, { method: 'DELETE' });
      if (res.ok) {
        setConfirmDelete(null);
        fetchTeam();
      } else {
        let msg = `Failed to remove user (HTTP ${res.status})`;
        try { const data = await res.json(); if (data?.error || data?.detail) msg = data.error || data.detail; } catch (_) {}
        alert(msg);
      }
    } catch (e) { alert('Network error'); }
  };

  const roleBadge = (role) => {
    const colors = {
      Admin: { bg: 'rgba(99,102,241,0.2)', color: '#a5b4fc', border: 'rgba(99,102,241,0.4)' },
      Agent: { bg: 'rgba(34,197,94,0.2)', color: '#4ade80', border: 'rgba(34,197,94,0.4)' },
      Viewer: { bg: 'rgba(234,179,8,0.2)', color: '#fde047', border: 'rgba(234,179,8,0.4)' },
    };
    const c = colors[role] || colors.Agent;
    return (
      <span style={{
        padding: '2px 10px', borderRadius: '12px', fontSize: '0.75rem', fontWeight: 600,
        background: c.bg, color: c.color, border: `1px solid ${c.border}`,
      }}>{role}</span>
    );
  };

  const cardStyle = {
    background: 'rgba(30,41,59,0.7)', border: '1px solid rgba(148,163,184,0.1)',
    borderRadius: '12px', padding: '24px',
  };

  return (
    <div style={{ padding: '24px', maxWidth: '900px', margin: '0 auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ margin: 0, color: '#f1f5f9' }}>Team Members</h2>
        <button
          onClick={() => setShowInvite(true)}
          style={{
            background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', border: 'none',
            borderRadius: '8px', color: '#fff', padding: '10px 20px', cursor: 'pointer',
            fontWeight: 600, fontSize: '0.85rem',
          }}
        >+ Invite Member</button>
      </div>

      {/* Invite Modal */}
      {showInvite && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.6)', display: 'flex',
          alignItems: 'center', justifyContent: 'center', zIndex: 1000,
        }} onClick={closeInvite}>
          <div style={{ ...cardStyle, width: '420px', maxWidth: '90vw' }} onClick={e => e.stopPropagation()}>
            <h3 style={{ margin: '0 0 16px', color: '#f1f5f9' }}>Invite Team Member</h3>
            <form onSubmit={handleInvite}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                <input
                  placeholder="Full Name" required value={inviteForm.full_name}
                  onChange={e => setInviteForm({ ...inviteForm, full_name: e.target.value })}
                  style={inputStyle}
                />
                <input
                  placeholder="Email" type="email" required value={inviteForm.email}
                  onChange={e => setInviteForm({ ...inviteForm, email: e.target.value })}
                  style={inputStyle}
                />
                <select
                  value={inviteForm.role}
                  onChange={e => setInviteForm({ ...inviteForm, role: e.target.value })}
                  style={inputStyle}
                >
                  <option value="Admin">Admin</option>
                  <option value="Agent">Agent</option>
                  <option value="Viewer">Viewer</option>
                </select>
                <input
                  placeholder="Password (min 8 chars)" type="password" required minLength={8} maxLength={128}
                  value={inviteForm.password}
                  onChange={e => setInviteForm({ ...inviteForm, password: e.target.value })}
                  style={inputStyle}
                />
                {inviteError && <div style={{ color: '#fca5a5', fontSize: '0.85rem' }}>{inviteError}</div>}
                <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end' }}>
                  <button type="button" onClick={closeInvite}
                    style={{ background: 'rgba(148,163,184,0.15)', border: '1px solid rgba(148,163,184,0.2)', borderRadius: '6px', color: '#94a3b8', padding: '8px 16px', cursor: 'pointer' }}>
                    Cancel
                  </button>
                  <button type="submit" disabled={inviteLoading}
                    style={{
                      background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', border: 'none',
                      borderRadius: '6px', color: '#fff', padding: '8px 20px', cursor: 'pointer', fontWeight: 600,
                      opacity: inviteLoading ? 0.6 : 1,
                    }}>
                    {inviteLoading ? 'Inviting...' : 'Send Invite'}
                  </button>
                </div>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Team Table */}
      <div style={cardStyle}>
        {loading ? (
          <div style={{ textAlign: 'center', color: '#94a3b8', padding: '40px' }}>Loading team...</div>
        ) : members.length === 0 ? (
          <div style={{ textAlign: 'center', color: '#94a3b8', padding: '40px' }}>No team members found.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid rgba(148,163,184,0.15)' }}>
                <th style={thStyle}>Name</th>
                <th style={thStyle}>Email</th>
                <th style={thStyle}>Role</th>
                <th style={thStyle}>Joined</th>
                <th style={{ ...thStyle, textAlign: 'right' }}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {members.map(m => {
                const isSelf = currentUser && currentUser.id === m.id;
                return (
                <tr key={m.id} style={{ borderBottom: '1px solid rgba(148,163,184,0.08)' }}>
                  <td style={tdStyle}>
                    {m.full_name || '-'}
                    {isSelf && (
                      <span style={{ marginLeft: '8px', fontSize: '0.7rem', color: '#a78bfa', fontWeight: 600 }}>(you)</span>
                    )}
                  </td>
                  <td style={tdStyle}>{m.email}</td>
                  <td style={tdStyle}>
                    <select
                      value={m.role}
                      disabled={isSelf}
                      title={isSelf ? "You cannot change your own role" : undefined}
                      onChange={e => handleRoleChange(m.id, e.target.value)}
                      style={{
                        background: 'rgba(30,41,59,0.9)', border: '1px solid rgba(148,163,184,0.2)',
                        borderRadius: '6px', color: isSelf ? '#64748b' : '#e2e8f0', padding: '4px 8px', fontSize: '0.8rem',
                        cursor: isSelf ? 'not-allowed' : 'pointer',
                        opacity: isSelf ? 0.6 : 1,
                      }}
                    >
                      <option value="Admin">Admin</option>
                      <option value="Agent">Agent</option>
                      <option value="Viewer">Viewer</option>
                    </select>
                  </td>
                  <td style={tdStyle}>
                    {m.created_at ? new Date(m.created_at).toLocaleDateString() : '-'}
                  </td>
                  <td style={{ ...tdStyle, textAlign: 'right' }}>
                    {currentUser && currentUser.id === m.id ? (
                      // No Remove button on the caller's own row — self-removal
                      // would lock them out (and could lock the org out if
                      // they're the only admin). Backend rejects it anyway.
                      // Issue #54.
                      <span style={{ color: '#64748b', fontSize: '0.75rem' }}>—</span>
                    ) : confirmDelete === m.id ? (
                      <span style={{ display: 'flex', gap: '6px', justifyContent: 'flex-end', alignItems: 'center' }}>
                        <span style={{ fontSize: '0.75rem', color: '#fca5a5' }}>Remove?</span>
                        <button onClick={() => handleDelete(m.id)}
                          style={{
                            background: 'rgba(239,68,68,0.2)', border: '1px solid rgba(239,68,68,0.4)',
                            borderRadius: '4px', color: '#fca5a5', padding: '3px 10px', cursor: 'pointer',
                            fontSize: '0.75rem', fontWeight: 600,
                          }}>Yes</button>
                        <button onClick={() => setConfirmDelete(null)}
                          style={{
                            background: 'rgba(148,163,184,0.15)', border: '1px solid rgba(148,163,184,0.2)',
                            borderRadius: '4px', color: '#94a3b8', padding: '3px 10px', cursor: 'pointer',
                            fontSize: '0.75rem',
                          }}>No</button>
                      </span>
                    ) : (
                      <button onClick={() => setConfirmDelete(m.id)}
                        style={{
                          background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.2)',
                          borderRadius: '4px', color: '#fca5a5', padding: '3px 10px', cursor: 'pointer',
                          fontSize: '0.75rem',
                        }}>Remove</button>
                    )}
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

const inputStyle = {
  background: 'rgba(15,23,42,0.8)', border: '1px solid rgba(148,163,184,0.2)',
  borderRadius: '8px', color: '#e2e8f0', padding: '10px 14px', fontSize: '0.9rem',
  outline: 'none', width: '100%', boxSizing: 'border-box',
};

const thStyle = {
  textAlign: 'left', padding: '10px 12px', color: '#94a3b8',
  fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.5px',
};

const tdStyle = {
  padding: '12px', color: '#e2e8f0', fontSize: '0.85rem',
};
