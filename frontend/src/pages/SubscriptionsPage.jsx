import React, { useState, useEffect } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { API_URL } from '../constants/api';

export default function SubscriptionsPage({ apiFetch }) {
  const { currentUser } = useAuth();
  const [email, setEmail] = useState('');
  const [expiresAt, setExpiresAt] = useState('');
  const [plan, setPlan] = useState('standard');
  const [minutes, setMinutes] = useState(100);
  const [isActive, setIsActive] = useState(true);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState(null);
  const [error, setError] = useState(null);
  const [lookupEmail, setLookupEmail] = useState('');
  const [subscription, setSubscription] = useState(null);
  const [lookupLoading, setLookupLoading] = useState(false);
  const [subscriptions, setSubscriptions] = useState([]);
  const [listLoading, setListLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');

  const fetchSubscriptions = async () => {
    setListLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/admin/subscriptions`);
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to load subscriptions');
      setSubscriptions(Array.isArray(data) ? data : []);
    } catch (err) {
      setError(err.message);
    }
    setListLoading(false);
  };

  // Default expiry to 30 days from now
  useEffect(() => {
    const d = new Date();
    d.setDate(d.getDate() + 30);
    setExpiresAt(d.toISOString().slice(0, 16));
  }, []);

  // Load all subscriptions on mount
  useEffect(() => {
    fetchSubscriptions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const showMessage = (text, type = 'success') => {
    setMessage({ text, type });
    setTimeout(() => setMessage(null), 5000);
  };

  const handleCreateOrUpdate = async (e) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/admin/subscriptions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          admin_email: email,
          expires_at: new Date(expiresAt).toISOString(),
          plan,
          minutes: Number(minutes) || 0,
          is_active: isActive,
        }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to save subscription');
      showMessage(`Subscription ${data.status} for ${data.admin_email} until ${new Date(data.expires_at).toLocaleDateString()} with ${data.minutes_available || 0} minutes`);
      setEmail('');
      setMinutes(100);
      fetchSubscriptions();
    } catch (err) {
      setError(err.message);
    }
    setLoading(false);
  };

  const handleLookup = async (e) => {
    e.preventDefault();
    setError(null);
    setSubscription(null);
    setLookupLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/admin/subscriptions/${encodeURIComponent(lookupEmail)}`);
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Subscription not found');
      setSubscription(data);
      // Pre-fill form for quick edit
      setEmail(data.admin_email);
      setPlan(['trial', 'manual', 'standard'].includes(data.plan) ? data.plan : 'standard');
      setMinutes(data.minutes_available || 0);
      setIsActive(data.is_active);
      const d = new Date(data.expires_at);
      setExpiresAt(d.toISOString().slice(0, 16));
    } catch (err) {
      setError(err.message);
    }
    setLookupLoading(false);
  };

  const inputStyle = {
    width: '100%',
    padding: '10px 12px',
    borderRadius: 8,
    border: '1px solid #d1d5db',
    fontSize: 14,
    outline: 'none',
    boxSizing: 'border-box',
  };

  const labelStyle = {
    display: 'block',
    marginBottom: 6,
    fontSize: 13,
    fontWeight: 600,
    color: '#374151',
  };

  const cardStyle = {
    background: '#fff',
    borderRadius: 12,
    padding: '1.5rem',
    boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
    border: '1px solid #e5e7eb',
    marginBottom: '1.5rem',
  };

  if (!currentUser?.is_super_admin) {
    return (
      <div style={{ padding: '2rem', textAlign: 'center', color: '#6b7280' }}>
        <h2>Access Denied</h2>
        <p>You do not have permission to manage subscriptions.</p>
      </div>
    );
  }

  return (
    <div style={{ padding: '1.5rem 2rem', maxWidth: 1000, margin: '0 auto' }}>
      <h1 style={{ fontSize: '1.6rem', fontWeight: 700, marginBottom: '1.5rem', color: '#111827' }}>
        🛡️ Subscription Management
      </h1>

      {message && (
        <div style={{
          background: message.type === 'success' ? 'rgba(34,197,94,0.12)' : 'rgba(245,158,11,0.12)',
          border: `1px solid ${message.type === 'success' ? 'rgba(34,197,94,0.4)' : 'rgba(245,158,11,0.4)'}`,
          color: message.type === 'success' ? '#15803d' : '#92400e',
          borderRadius: 8,
          padding: '12px 16px',
          marginBottom: '1rem',
          fontSize: 14,
        }}>
          {message.text}
        </div>
      )}

      {error && (
        <div style={{
          background: 'rgba(239,68,68,0.12)',
          border: '1px solid rgba(239,68,68,0.4)',
          color: '#b91c1c',
          borderRadius: 8,
          padding: '12px 16px',
          marginBottom: '1rem',
          fontSize: 14,
        }}>
          {error}
        </div>
      )}

      {/* Lookup Card */}
      <div style={cardStyle}>
        <h2 style={{ fontSize: '1.1rem', fontWeight: 700, marginBottom: '1rem', color: '#111827' }}>
          🔍 Lookup Subscription
        </h2>
        <form onSubmit={handleLookup} style={{ display: 'flex', gap: 12, alignItems: 'flex-end' }}>
          <div style={{ flex: 1 }}>
            <label style={labelStyle}>Admin Email</label>
            <input
              type="email"
              required
              value={lookupEmail}
              onChange={e => setLookupEmail(e.target.value)}
              placeholder="admin@client.com"
              style={inputStyle}
            />
          </div>
          <button
            type="submit"
            disabled={lookupLoading}
            style={{
              background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
              border: 'none',
              borderRadius: 8,
              color: '#fff',
              cursor: 'pointer',
              fontSize: 14,
              fontWeight: 600,
              padding: '10px 20px',
              whiteSpace: 'nowrap',
            }}
          >
            {lookupLoading ? 'Searching...' : 'Lookup'}
          </button>
        </form>

        {subscription && (
          <div style={{
            marginTop: '1rem',
            padding: '1rem',
            background: '#f9fafb',
            borderRadius: 8,
            border: '1px solid #e5e7eb',
          }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 12 }}>
              <div>
                <span style={{ fontSize: 12, color: '#6b7280' }}>Email</span>
                <div style={{ fontWeight: 600, fontSize: 14 }}>{subscription.admin_email}</div>
              </div>
              <div>
                <span style={{ fontSize: 12, color: '#6b7280' }}>Plan</span>
                <div style={{ fontWeight: 600, fontSize: 14 }}>{subscription.plan}</div>
              </div>
              <div>
                <span style={{ fontSize: 12, color: '#6b7280' }}>Status</span>
                <div style={{ fontWeight: 600, fontSize: 14 }}>
                  <span style={{
                    display: 'inline-block',
                    padding: '2px 8px',
                    borderRadius: 12,
                    fontSize: 12,
                    background: subscription.status === 'active' ? '#dcfce7' : subscription.status === 'expired' ? '#fee2e2' : '#fef3c7',
                    color: subscription.status === 'active' ? '#166534' : subscription.status === 'expired' ? '#991b1b' : '#92400e',
                  }}>
                    {subscription.status}
                  </span>
                </div>
              </div>
              <div>
                <span style={{ fontSize: 12, color: '#6b7280' }}>Expires At</span>
                <div style={{ fontWeight: 600, fontSize: 14 }}>{new Date(subscription.expires_at).toLocaleString()}</div>
              </div>
              <div>
                <span style={{ fontSize: 12, color: '#6b7280' }}>Minutes</span>
                <div style={{ fontWeight: 600, fontSize: 14 }}>{subscription.minutes_available || 0}</div>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Create / Update Card */}
      <div style={cardStyle}>
        <h2 style={{ fontSize: '1.1rem', fontWeight: 700, marginBottom: '1rem', color: '#111827' }}>
          ✏️ Create or Update Subscription
        </h2>
        <form onSubmit={handleCreateOrUpdate}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: 16, marginBottom: 16 }}>
            <div>
              <label style={labelStyle}>Admin Email *</label>
              <input
                type="email"
                required
                value={email}
                onChange={e => setEmail(e.target.value)}
                placeholder="admin@client.com"
                style={inputStyle}
              />
            </div>
            <div>
              <label style={labelStyle}>Expiration Date *</label>
              <input
                type="datetime-local"
                required
                value={expiresAt}
                onChange={e => setExpiresAt(e.target.value)}
                style={inputStyle}
              />
            </div>
            <div>
              <label style={labelStyle}>Plan</label>
              <select
                value={plan}
                onChange={e => setPlan(e.target.value)}
                style={inputStyle}
              >
                <option value="trial">Trial</option>
                <option value="standard">Standard (AI features enabled)</option>
                <option value="manual">Manual calls only (AI features hidden)</option>
              </select>
            </div>
            <div>
              <label style={labelStyle}>Minutes</label>
              <input
                type="number"
                min="0"
                step="1"
                value={minutes}
                onChange={e => setMinutes(e.target.value)}
                placeholder="100"
                style={inputStyle}
              />
            </div>
          </div>

          <div style={{ marginBottom: 16 }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 14, color: '#374151' }}>
              <input
                type="checkbox"
                checked={isActive}
                onChange={e => setIsActive(e.target.checked)}
              />
              Active
            </label>
          </div>

          <button
            type="submit"
            disabled={loading}
            style={{
              background: 'linear-gradient(135deg, #10b981, #059669)',
              border: 'none',
              borderRadius: 8,
              color: '#fff',
              cursor: 'pointer',
              fontSize: 14,
              fontWeight: 600,
              padding: '12px 24px',
            }}
          >
            {loading ? 'Saving...' : 'Save Subscription'}
          </button>
        </form>
      </div>

      {/* All Subscriptions */}
      <div style={cardStyle}>
        <h2 style={{ fontSize: '1.1rem', fontWeight: 700, marginBottom: '1rem', color: '#111827' }}>
          All Subscriptions
        </h2>
        <div style={{ marginBottom: '1rem' }}>
          <input
            type="text"
            placeholder="Search by email..."
            value={searchQuery}
            onChange={e => setSearchQuery(e.target.value)}
            style={inputStyle}
          />
        </div>
        {listLoading ? (
          <div style={{ textAlign: 'center', padding: '1rem', color: '#6b7280' }}>Loading...</div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
              <thead>
                <tr style={{ borderBottom: '1px solid #e5e7eb', textAlign: 'left' }}>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Email</th>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Plan</th>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Status</th>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Minutes</th>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Expires At</th>
                  <th style={{ padding: '10px 8px', color: '#374151', fontWeight: 600 }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {subscriptions
                  .filter(s => s.admin_email.toLowerCase().includes(searchQuery.toLowerCase()))
                  .map(s => (
                    <tr key={s.admin_email} style={{ borderBottom: '1px solid #f3f4f6' }}>
                      <td style={{ padding: '10px 8px' }}>{s.admin_email}</td>
                      <td style={{ padding: '10px 8px', textTransform: 'capitalize' }}>{s.plan}</td>
                      <td style={{ padding: '10px 8px' }}>
                        <span style={{
                          display: 'inline-block',
                          padding: '2px 8px',
                          borderRadius: 12,
                          fontSize: 12,
                          fontWeight: 600,
                          background: s.status === 'active' ? '#dcfce7' : s.status === 'expired' ? '#fee2e2' : '#fef3c7',
                          color: s.status === 'active' ? '#166534' : s.status === 'expired' ? '#991b1b' : '#92400e',
                        }}>
                          {s.status}
                        </span>
                      </td>
                      <td style={{ padding: '10px 8px' }}>{s.minutes_available || 0}</td>
                      <td style={{ padding: '10px 8px' }}>{new Date(s.expires_at).toLocaleString()}</td>
                      <td style={{ padding: '10px 8px' }}>
                        <button
                          onClick={() => {
                            setEmail(s.admin_email);
                            setPlan(['trial', 'manual', 'standard'].includes(s.plan) ? s.plan : 'standard');
                            setMinutes(s.minutes_available || 0);
                            setIsActive(s.is_active);
                            const d = new Date(s.expires_at);
                            setExpiresAt(d.toISOString().slice(0, 16));
                            window.scrollTo({ top: 0, behavior: 'smooth' });
                          }}
                          style={{
                            background: 'rgba(99,102,241,0.08)',
                            border: '1px solid rgba(99,102,241,0.3)',
                            color: '#6366f1',
                            borderRadius: 6,
                            padding: '4px 10px',
                            cursor: 'pointer',
                            fontSize: 12,
                            fontWeight: 600,
                          }}
                        >
                          Edit
                        </button>
                      </td>
                    </tr>
                  ))}
                {subscriptions.filter(s => s.admin_email.toLowerCase().includes(searchQuery.toLowerCase())).length === 0 && (
                  <tr>
                    <td colSpan={6} style={{ padding: '20px 8px', textAlign: 'center', color: '#6b7280' }}>
                      {searchQuery ? 'No subscriptions match your search.' : 'No subscriptions found.'}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Instructions */}
      <div style={{ ...cardStyle, background: '#f9fafb' }}>
        <h3 style={{ fontSize: '1rem', fontWeight: 700, marginBottom: 8, color: '#111827' }}>How it works</h3>
        <ul style={{ margin: 0, paddingLeft: 18, color: '#4b5563', fontSize: 14, lineHeight: 1.6 }}>
          <li>Admins without a subscription will be blocked at login.</li>
          <li>Admins with an expired subscription will see a renewal prompt.</li>
          <li>Trial signups receive 100 starting minutes.</li>
          <li>Use the <strong>Lookup</strong> form to check an admin's current status.</li>
          <li>Use the <strong>Create/Update</strong> form to set expiry, plan, status, and minutes.</li>
        </ul>
      </div>
    </div>
  );
}
