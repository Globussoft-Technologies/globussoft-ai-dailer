import { useState, useEffect, useCallback } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { useToast, useConfirm } from '../contexts/UIContext';
import { isAdmin, isTeamLeader, ROLES } from '../utils/roles';
import UserProviderAccountsModal from '../components/UserProviderAccountsModal';

const T = {
  bg: '#f4f5f9', card: '#ffffff', border: '#e5e7eb',
  accent: '#6366f1', green: '#10b981', amber: '#f59e0b',
  red: '#ef4444', text: '#111827', sub: '#374151', muted: '#9ca3af',
  font: "'DM Sans', sans-serif",
};

const cardStyle = {
  background: T.card, border: `1px solid ${T.border}`,
  borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06), 0 4px 12px rgba(0,0,0,0.04)',
  padding: '24px 28px',
};

const inputStyle = {
  background: '#f9fafb', border: `1px solid ${T.border}`,
  borderRadius: 8, color: T.text, padding: '10px 14px', fontSize: 13,
  outline: 'none', width: '100%', boxSizing: 'border-box', fontFamily: T.font,
};

const selectStyle = {
  ...inputStyle,
  appearance: 'none',
  backgroundImage: `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%239ca3af' stroke-width='2'%3E%3Cpath d='M6 9l6 6 6-6'/%3E%3C/svg%3E")`,
  backgroundRepeat: 'no-repeat', backgroundPosition: 'right 12px center',
};

const thStyle = {
  textAlign: 'left', padding: '0 12px 12px', color: T.muted,
  fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.07em',
  borderBottom: `1px solid ${T.border}`,
};

const tdStyle = {
  padding: '12px', color: T.sub, fontSize: 13, borderBottom: `1px solid ${T.border}`,
  verticalAlign: 'middle',
};

const btnPrimary = {
  background: T.accent, color: '#fff', border: 'none', borderRadius: 8,
  padding: '8px 16px', fontSize: 13, fontWeight: 600, cursor: 'pointer', fontFamily: T.font,
};

const btnSecondary = {
  background: '#fff', color: T.sub, border: `1px solid ${T.border}`, borderRadius: 8,
  padding: '6px 12px', fontSize: 12, fontWeight: 500, cursor: 'pointer', fontFamily: T.font,
};

const btnDanger = {
  ...btnSecondary, color: T.red, borderColor: '#fecaca',
};

const badge = (color, bg) => ({
  display: 'inline-block', padding: '3px 10px', borderRadius: 12,
  fontSize: 11, fontWeight: 600, color, background: bg,
});

export default function UserManagementPage({ apiFetch, API_URL, currentUser }) {
  const { currentUser: authUser, hasPermission } = useAuth();
  const user = currentUser || authUser;
  const userRole = user?.role || 'Agent';
  const canManageProviderAccounts = hasPermission('provider_accounts.own');
  const toast = useToast();
  const confirmDialog = useConfirm();

  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showAdd, setShowAdd] = useState(false);
  const [showEdit, setShowEdit] = useState(false);
  const [editingUser, setEditingUser] = useState(null);
  const [providerAccountsUser, setProviderAccountsUser] = useState(null);

  const [form, setForm] = useState({
    email: '', password: '', full_name: '', role: ROLES.AGENT, manager_id: '',
    setup_calling: false,
    provider_account: {
      provider: 'exotel', name: '', api_key: '', api_token: '', api_secret: '',
      account_sid: '', caller_id: '', app_id: '', app_type: 'exoml', direction: 'outbound', region: '', subdomain: '',
    },
  });

  const fetchUsers = useCallback(async () => {
    setLoading(true);
    try {
      if (isTeamLeader(userRole)) {
        const res = await apiFetch(`${API_URL}/users/my-agents`);
        if (res.ok) setUsers(await res.json());
      } else {
        const res = await apiFetch(`${API_URL}/users`);
        if (res.ok) setUsers(await res.json());
      }
    } catch (e) { console.error('User fetch error:', e); }
    setLoading(false);
  }, [apiFetch, API_URL, userRole]);

  useEffect(() => { fetchUsers(); }, [fetchUsers]);

  const resetForm = () => {
    setForm({
      email: '', password: '', full_name: '', role: ROLES.AGENT, manager_id: '',
      setup_calling: false,
      provider_account: {
        provider: 'exotel', name: '', api_key: '', api_token: '', api_secret: '',
        account_sid: '', caller_id: '', app_id: '', app_type: 'exoml', direction: 'outbound', region: '', subdomain: '',
      },
    });
  };

  const openAdd = () => { resetForm(); setShowAdd(true); };
  const closeAdd = () => { setShowAdd(false); resetForm(); };

  const openEdit = (u) => {
    setEditingUser(u);
    setForm({
      email: u.email,
      password: '',
      full_name: u.full_name || '',
      role: u.role || ROLES.AGENT,
      manager_id: u.manager_id || '',
      is_active: u.is_active !== false,
    });
    setShowEdit(true);
  };
  const closeEdit = () => { setShowEdit(false); setEditingUser(null); resetForm(); };

  const handleCreate = async (e) => {
    e.preventDefault();
    const payload = { ...form };
    if (payload.manager_id) payload.manager_id = Number(payload.manager_id);
    else delete payload.manager_id;
    if (!isAdmin(userRole)) {
      // Team Leader creating an Agent under themselves.
      delete payload.role;
      delete payload.manager_id;
    }
    delete payload.setup_calling;
    if (!form.setup_calling) {
      delete payload.provider_account;
    }
    try {
      const url = isAdmin(userRole) ? `${API_URL}/users` : `${API_URL}/users/agent`;
      const res = await apiFetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast('User created successfully', 'success');
        closeAdd();
        fetchUsers();
      } else {
        toast(data.error || data.detail || 'Failed to create user', 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  const handleUpdate = async (e) => {
    e.preventDefault();
    if (!editingUser) return;
    const payload = {
      full_name: form.full_name,
      role: form.role,
      is_active: form.is_active,
    };
    if (form.manager_id) payload.manager_id = Number(form.manager_id);
    else payload.manager_id = 0;

    if (isTeamLeader(userRole)) {
      // Team leaders can only update name/active for their agents.
      const tlPayload = { full_name: form.full_name, is_active: form.is_active };
      try {
        const res = await apiFetch(`${API_URL}/users/${editingUser.id}/agent`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(tlPayload),
        });
        const data = await res.json().catch(() => ({}));
        if (res.ok) {
          toast('User updated', 'success');
          closeEdit();
          fetchUsers();
        } else {
          toast(data.error || 'Failed to update user', 'error');
        }
      } catch { toast('Network error', 'error'); }
      return;
    }

    try {
      const res = await apiFetch(`${API_URL}/users/${editingUser.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast('User updated', 'success');
        closeEdit();
        fetchUsers();
      } else {
        toast(data.error || 'Failed to update user', 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  const handleToggleActive = async (u) => {
    const next = !u.is_active;
    const action = next ? 'Enable' : 'Disable';
    const ok = await confirmDialog({
      title: `${action} user`,
      message: `${action} login for ${u.email}?`,
      okText: action,
      danger: !next,
    });
    if (!ok) return;
    try {
      const res = await apiFetch(`${API_URL}/users/${u.id}/toggle-active`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ is_active: next }),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast(`User ${next ? 'enabled' : 'disabled'}`, 'success');
        fetchUsers();
      } else {
        toast(data.error || `Failed to ${action.toLowerCase()} user`, 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  const handleDelete = async (u) => {
    const ok = await confirmDialog({
      title: 'Delete user',
      message: `Permanently delete ${u.email}? This cannot be undone.`,
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      const res = await apiFetch(`${API_URL}/users/${u.id}`, { method: 'DELETE' });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast('User deleted', 'success');
        fetchUsers();
      } else {
        toast(data.error || 'Failed to delete user', 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  const managerOptions = users.filter(u => u.role === ROLES.TEAM_LEADER || u.role === ROLES.ADMIN);

  const renderTable = () => (
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr>
          <th style={thStyle}>Name</th>
          <th style={thStyle}>Email</th>
          <th style={thStyle}>Role</th>
          <th style={thStyle}>Manager</th>
          <th style={thStyle}>Status</th>
          <th style={thStyle}>Actions</th>
        </tr>
      </thead>
      <tbody>
        {users.map(u => {
          const manager = users.find(m => m.id === u.manager_id);
          return (
            <tr key={u.id}>
              <td style={tdStyle}>{u.full_name || '—'}</td>
              <td style={tdStyle}>{u.email}</td>
              <td style={tdStyle}>
                <span style={badge(
                  u.role === ROLES.ADMIN ? '#7c3aed' : u.role === ROLES.TEAM_LEADER ? '#0369a1' : '#047857',
                  u.role === ROLES.ADMIN ? '#ede9fe' : u.role === ROLES.TEAM_LEADER ? '#e0f2fe' : '#d1fae5'
                )}>
                  {u.role === ROLES.TEAM_LEADER ? 'Team Leader' : u.role}
                </span>
              </td>
              <td style={tdStyle}>{manager ? (manager.full_name || manager.email) : '—'}</td>
              <td style={tdStyle}>
                <span style={u.is_active !== false ? badge('#047857', '#d1fae5') : badge('#b91c1c', '#fee2e2')}>
                  {u.is_active !== false ? 'Active' : 'Disabled'}
                </span>
              </td>
              <td style={tdStyle}>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button style={btnSecondary} onClick={() => openEdit(u)}>Edit</button>
                  {canManageProviderAccounts && (
                    <button style={btnSecondary} onClick={() => setProviderAccountsUser(u)}>Provider Accounts</button>
                  )}
                  <button style={btnSecondary} onClick={() => handleToggleActive(u)}>
                    {u.is_active !== false ? 'Disable' : 'Enable'}
                  </button>
                  {isAdmin(userRole) && (
                    <button style={btnDanger} onClick={() => handleDelete(u)}>Delete</button>
                  )}
                </div>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );

  return (
    <div style={{ padding: 24, fontFamily: T.font, background: T.bg, minHeight: 'calc(100vh - 56px)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20 }}>
        <h1 style={{ margin: 0, fontSize: 22, color: T.text }}>
          {isTeamLeader(userRole) ? 'My Agents' : 'User Management'}
        </h1>
        <button style={btnPrimary} onClick={openAdd}>
          {isTeamLeader(userRole) ? 'Add Agent' : 'Add User'}
        </button>
      </div>

      <div style={cardStyle}>
        {loading ? (
          <div style={{ color: T.muted, fontSize: 13 }}>Loading...</div>
        ) : users.length === 0 ? (
          <div style={{ color: T.muted, fontSize: 13 }}>No users found.</div>
        ) : renderTable()}
      </div>

      {showAdd && (
        <Modal title={isTeamLeader(userRole) ? 'Add Agent' : 'Add User'} onClose={closeAdd}>
          <form onSubmit={handleCreate}>
            <Field label="Email" type="email" value={form.email} onChange={v => setForm({ ...form, email: v })} required />
            <Field label="Full Name" value={form.full_name} onChange={v => setForm({ ...form, full_name: v })} />
            <Field label="Password" type="password" value={form.password} onChange={v => setForm({ ...form, password: v })} required revealable />
            {isAdmin(userRole) && (
              <>
                <Select label="Role" value={form.role} onChange={v => setForm({ ...form, role: v })} options={[
                  { value: ROLES.AGENT, label: 'Agent' },
                  { value: ROLES.EXECUTIVE, label: 'Executive' },
                  { value: ROLES.TEAM_LEADER, label: 'Team Leader' },
                  { value: ROLES.ADMIN, label: 'Admin' },
                ]} />
                {(form.role === ROLES.AGENT || form.role === ROLES.EXECUTIVE) && (
                  <Select label="Manager" value={form.manager_id} onChange={v => setForm({ ...form, manager_id: v })} options={[
                    { value: '', label: 'None' },
                    ...managerOptions.map(m => ({ value: String(m.id), label: m.full_name || m.email })),
                  ]} />
                )}
              </>
            )}
            {(form.role !== ROLES.ADMIN || isTeamLeader(userRole)) && (
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 12, fontSize: 13, color: T.sub, cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={!!form.setup_calling}
                  onChange={e => setForm({ ...form, setup_calling: e.target.checked })}
                />
                Set up calling configuration (Exotel/Twilio) for this user
              </label>
            )}
            {form.setup_calling && (
              <div style={{ marginTop: 16, padding: 16, background: '#f9fafb', borderRadius: 8, border: `1px solid ${T.border}` }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: T.text, marginBottom: 12 }}>Calling Provider Account</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                  <Field label="Account Name" value={form.provider_account?.name || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, name: v } })} required />
                  <Select label="Provider" value={form.provider_account?.provider || 'exotel'} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, provider: v } })} options={[
                    { value: 'exotel', label: 'Exotel' },
                    { value: 'tata', label: 'Tata Tele' },
                    { value: 'twilio', label: 'Twilio' },
                  ]} />
                  <Field label={form.provider_account?.provider === 'tata' ? 'API Token' : 'API Key / Auth Token'} value={form.provider_account?.api_key || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, api_key: v } })} required />
                  {form.provider_account?.provider !== 'tata' && (
                    <Field label="API Token / API Key SID" value={form.provider_account?.api_token || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, api_token: v } })} required />
                  )}
                  {form.provider_account?.provider === 'twilio' && (
                    <Field label="API Secret" value={form.provider_account?.api_secret || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, api_secret: v } })} required />
                  )}
                  {form.provider_account?.provider !== 'tata' && (
                    <Field label="Account SID" value={form.provider_account?.account_sid || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, account_sid: v } })} required />
                  )}
                  <Field label={form.provider_account?.provider === 'tata' ? 'Caller ID' : 'Caller ID / From Number'} value={form.provider_account?.caller_id || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, caller_id: v } })} required />
                  <Field label={form.provider_account?.provider === 'tata' ? 'Agent Number' : 'App ID / TwiML App SID'} value={form.provider_account?.app_id || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, app_id: v } })} required={form.provider_account?.provider === 'tata'} />
                  {form.provider_account?.provider === 'exotel' && (
                    <Select label="App Type" value={form.provider_account?.app_type || 'exoml'} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, app_type: v } })} options={[
                      { value: 'exoml', label: 'ExoML (legacy XML)' },
                      { value: 'voicebot', label: 'Voicebot (AgentStream JSON)' },
                    ]} />
                  )}
                  <Field label="Region" value={form.provider_account?.region || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, region: v } })} placeholder="e.g. in" />
                  <Field label="Subdomain" value={form.provider_account?.subdomain || ''} onChange={v => setForm({ ...form, provider_account: { ...form.provider_account, subdomain: v } })} placeholder="e.g. myaccount" />
                </div>
              </div>
            )}
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 20 }}>
              <button type="button" style={btnSecondary} onClick={closeAdd}>Cancel</button>
              <button type="submit" style={btnPrimary}>Create</button>
            </div>
          </form>
        </Modal>
      )}

      {showEdit && editingUser && (
        <Modal title="Edit User" onClose={closeEdit}>
          <form onSubmit={handleUpdate}>
            <div style={{ marginBottom: 12, fontSize: 13, color: T.sub }}>
              <strong>{editingUser.email}</strong>
            </div>
            <Field label="Full Name" value={form.full_name} onChange={v => setForm({ ...form, full_name: v })} />
            {isAdmin(userRole) && (
              <>
                <Select label="Role" value={form.role} onChange={v => setForm({ ...form, role: v })} options={[
                  { value: ROLES.AGENT, label: 'Agent' },
                  { value: ROLES.EXECUTIVE, label: 'Executive' },
                  { value: ROLES.TEAM_LEADER, label: 'Team Leader' },
                  { value: ROLES.ADMIN, label: 'Admin' },
                ]} />
                {(form.role === ROLES.AGENT || form.role === ROLES.EXECUTIVE) && (
                  <Select label="Manager" value={form.manager_id} onChange={v => setForm({ ...form, manager_id: v })} options={[
                    { value: '', label: 'None' },
                    ...managerOptions.map(m => ({ value: String(m.id), label: m.full_name || m.email })),
                  ]} />
                )}
              </>
            )}
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 12, fontSize: 13, color: T.sub, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={!!form.is_active}
                onChange={e => setForm({ ...form, is_active: e.target.checked })}
              />
              Active
            </label>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 20 }}>
              <button type="button" style={btnSecondary} onClick={closeEdit}>Cancel</button>
              <button type="submit" style={btnPrimary}>Save</button>
            </div>
          </form>
        </Modal>
      )}

      {providerAccountsUser && (
        <UserProviderAccountsModal
          user={providerAccountsUser}
          apiFetch={apiFetch}
          API_URL={API_URL}
          onClose={() => setProviderAccountsUser(null)}
        />
      )}
    </div>
  );
}

function Modal({ title, children, onClose }) {
  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 200,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }} onClick={onClose}>
      <div style={{
        background: T.card, borderRadius: 12, padding: '24px 28px', width: 420, maxWidth: '90vw',
        boxShadow: '0 20px 40px rgba(0,0,0,0.2)',
      }} onClick={e => e.stopPropagation()}>
        <h3 style={{ margin: '0 0 16px', fontSize: 16, color: T.text }}>{title}</h3>
        {children}
      </div>
    </div>
  );
}

function Field({ label, type = 'text', value, onChange, required, revealable = false }) {
  const [showValue, setShowValue] = useState(false);
  const inputType = revealable && type === 'password' && showValue ? 'text' : type;
  return (
    <div style={{ marginBottom: 14 }}>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: T.sub, marginBottom: 6 }}>{label}</label>
      <div style={{ position: 'relative' }}>
        <input
          type={inputType}
          style={revealable ? { ...inputStyle, paddingRight: 42 } : inputStyle}
          value={value}
          onChange={e => onChange(e.target.value)}
          required={required}
        />
        {revealable && type === 'password' && (
          <button
            type="button"
            onClick={() => setShowValue(v => !v)}
            aria-label={showValue ? 'Hide password' : 'Show password'}
            title={showValue ? 'Hide password' : 'Show password'}
            style={{
              position: 'absolute',
              right: 8,
              top: '50%',
              transform: 'translateY(-50%)',
              width: 28,
              height: 28,
              border: 'none',
              borderRadius: 6,
              background: 'transparent',
              color: T.muted,
              cursor: 'pointer',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 15,
            }}
          >
            {showValue ? '🙈' : '👁'}
          </button>
        )}
      </div>
    </div>
  );
}

function Select({ label, value, onChange, options }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: T.sub, marginBottom: 6 }}>{label}</label>
      <select style={selectStyle} value={value} onChange={e => onChange(e.target.value)}>
        {options.map(o => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}
