import { useState, useEffect, useCallback } from 'react';
import { useToast, useConfirm } from '../contexts/UIContext';

const T = {
  bg: '#f4f5f9', card: '#ffffff', border: '#e5e7eb',
  accent: '#6366f1', green: '#10b981', amber: '#f59e0b',
  red: '#ef4444', text: '#111827', sub: '#374151', muted: '#9ca3af',
  font: "'DM Sans', sans-serif",
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

const thStyle = {
  textAlign: 'left', padding: '0 12px 12px', color: T.muted,
  fontSize: 10, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.07em',
  borderBottom: `1px solid ${T.border}`,
};

const tdStyle = {
  padding: '12px', color: T.sub, fontSize: 13, borderBottom: `1px solid ${T.border}`,
  verticalAlign: 'middle',
};

const initialForm = {
  provider: 'exotel', name: '', api_key: '', api_token: '', api_secret: '',
  account_sid: '', caller_id: '', app_id: '', app_type: 'exoml', direction: 'outbound', region: '', subdomain: '',
};

export default function UserProviderAccountsModal({ user, apiFetch, API_URL, onClose }) {
  const toast = useToast();
  const confirmDialog = useConfirm();
  const [accounts, setAccounts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState(null);
  const [form, setForm] = useState(initialForm);

  useEffect(() => {
    let cancelled = false;
    apiFetch(`${API_URL}/users/${user.id}/provider-accounts`)
      .then(async res => {
        if (cancelled) return;
        if (res.ok) setAccounts(await res.json());
      })
      .catch(e => { if (!cancelled) console.error('Provider accounts fetch error:', e); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [apiFetch, API_URL, user.id]);

  const reloadAccounts = useCallback(() => {
    apiFetch(`${API_URL}/users/${user.id}/provider-accounts`)
      .then(async res => { if (res.ok) setAccounts(await res.json()); })
      .catch(e => console.error('Provider accounts reload error:', e));
  }, [apiFetch, API_URL, user.id]);

  const resetForm = () => { setForm(initialForm); setEditing(null); };
  const openAdd = () => { resetForm(); setShowForm(true); };
  const openEdit = (a) => {
    setForm({
      provider: a.provider || 'exotel', name: a.name || '', api_key: a.api_key || '',
      api_token: a.api_token || '', api_secret: a.api_secret || '',
      account_sid: a.account_sid || '', caller_id: a.caller_id || '',
      app_id: a.app_id || '', app_type: a.app_type || 'exoml',
      direction: a.direction || 'outbound',
      region: a.region || '', subdomain: a.subdomain || '',
    });
    setEditing(a);
    setShowForm(true);
  };
  const closeForm = () => { setShowForm(false); resetForm(); };

  const handleSubmit = async (e) => {
    e.preventDefault();
    const payload = { ...form };
    const url = editing
      ? `${API_URL}/users/${user.id}/provider-accounts/${editing.id}`
      : `${API_URL}/users/${user.id}/provider-accounts`;
    try {
      const res = await apiFetch(url, {
        method: editing ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast(editing ? 'Provider account updated' : 'Provider account created', 'success');
        closeForm();
        reloadAccounts();
      } else {
        toast(data.error || 'Failed to save provider account', 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  const handleDelete = async (a) => {
    const ok = await confirmDialog({
      title: 'Delete provider account',
      message: `Remove ${a.name} for ${user.email}?`,
      okText: 'Delete',
      danger: true,
    });
    if (!ok) return;
    try {
      const res = await apiFetch(`${API_URL}/users/${user.id}/provider-accounts/${a.id}`, { method: 'DELETE' });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast('Provider account deleted', 'success');
        reloadAccounts();
      } else {
        toast(data.error || 'Failed to delete provider account', 'error');
      }
    } catch { toast('Network error', 'error'); }
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 200,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
    }} onClick={onClose}>
      <div style={{
        background: T.card, borderRadius: 12, padding: '24px 28px', width: 760, maxWidth: '95vw',
        maxHeight: '90vh', overflow: 'auto', boxShadow: '0 20px 40px rgba(0,0,0,0.2)',
      }} onClick={e => e.stopPropagation()}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
          <h3 style={{ margin: 0, fontSize: 16, color: T.text }}>
            Provider Accounts — {user.full_name || user.email}
          </h3>
          <button style={btnSecondary} onClick={onClose}>Close</button>
        </div>
        <p style={{ margin: '0 0 16px', fontSize: 12, color: T.muted }}>
          Personal Exotel/Tata/Twilio accounts owned by this user. When present, their outbound calls use these credentials instead of the org/campaign default.
        </p>

        {!showForm && (
          <button style={{ ...btnPrimary, marginBottom: 16 }} onClick={openAdd}>Add Account</button>
        )}

        {showForm && (
          <form onSubmit={handleSubmit} style={{ marginBottom: 20, padding: 16, background: '#f9fafb', borderRadius: 8, border: `1px solid ${T.border}` }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <Field label="Name" value={form.name} onChange={v => setForm({ ...form, name: v })} required />
              <Select label="Provider" value={form.provider} onChange={v => setForm({ ...form, provider: v })} options={[
                { value: 'exotel', label: 'Exotel' },
                { value: 'tata', label: 'Tata Tele' },
                { value: 'twilio', label: 'Twilio' },
              ]} />
              <Field label={form.provider === 'tata' ? 'API Token' : 'API Key / Auth Token'} value={form.api_key} onChange={v => setForm({ ...form, api_key: v })} required />
              {form.provider !== 'tata' && (
                <Field label="API Token / API Key SID" value={form.api_token} onChange={v => setForm({ ...form, api_token: v })} required />
              )}
              {form.provider === 'twilio' && (
                <Field label="API Secret" value={form.api_secret} onChange={v => setForm({ ...form, api_secret: v })} required />
              )}
              {form.provider !== 'tata' && (
                <Field label="Account SID" value={form.account_sid} onChange={v => setForm({ ...form, account_sid: v })} required />
              )}
              <Field label={form.provider === 'tata' ? 'Caller ID' : 'Caller ID / From Number'} value={form.caller_id} onChange={v => setForm({ ...form, caller_id: v })} required />
              <Field label={form.provider === 'tata' ? 'Agent Number' : 'App ID / TwiML App SID'} value={form.app_id} onChange={v => setForm({ ...form, app_id: v })} required={form.provider === 'tata'} />
              {form.provider === 'exotel' && (
                <Select label="App Type" value={form.app_type} onChange={v => setForm({ ...form, app_type: v })} options={[
                  { value: 'exoml', label: 'ExoML (legacy XML)' },
                  { value: 'voicebot', label: 'Voicebot (AgentStream JSON)' },
                ]} />
              )}
              <Field label="Region" value={form.region} onChange={v => setForm({ ...form, region: v })} placeholder="e.g. in" />
              <Field label="Subdomain" value={form.subdomain} onChange={v => setForm({ ...form, subdomain: v })} placeholder="e.g. myaccount" />
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 16 }}>
              <button type="button" style={btnSecondary} onClick={closeForm}>Cancel</button>
              <button type="submit" style={btnPrimary}>{editing ? 'Update' : 'Create'}</button>
            </div>
          </form>
        )}

        {loading ? (
          <div style={{ color: T.muted, fontSize: 13 }}>Loading...</div>
        ) : accounts.length === 0 ? (
          <div style={{ color: T.muted, fontSize: 13 }}>No personal provider accounts. Outbound calls will use the org/campaign default.</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                <th style={thStyle}>Name</th>
                <th style={thStyle}>Provider</th>
                <th style={thStyle}>Caller ID</th>
                <th style={thStyle}>App Type</th>
                <th style={thStyle}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {accounts.map(a => (
                <tr key={a.id}>
                  <td style={tdStyle}>{a.name}</td>
                  <td style={tdStyle}>{a.provider}</td>
                  <td style={tdStyle}>{a.caller_id}</td>
                  <td style={tdStyle}>{a.app_type || 'exoml'}</td>
                  <td style={tdStyle}>
                    <div style={{ display: 'flex', gap: 8 }}>
                      <button style={btnSecondary} onClick={() => openEdit(a)}>Edit</button>
                      <button style={btnDanger} onClick={() => handleDelete(a)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

function Field({ label, value, onChange, required, placeholder }) {
  return (
    <div>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: T.sub, marginBottom: 6 }}>{label}</label>
      <input
        type="text"
        style={inputStyle}
        value={value}
        onChange={e => onChange(e.target.value)}
        required={required}
        placeholder={placeholder || ''}
      />
    </div>
  );
}

function Select({ label, value, onChange, options }) {
  return (
    <div>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 600, color: T.sub, marginBottom: 6 }}>{label}</label>
      <select style={selectStyle} value={value} onChange={e => onChange(e.target.value)}>
        {options.map(o => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
    </div>
  );
}
