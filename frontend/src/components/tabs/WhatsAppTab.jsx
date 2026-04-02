import React, { useState, useEffect, useRef, useCallback } from 'react';
import { formatDateTime, formatTime } from '../../utils/dateFormat';

const PROVIDERS = [
  { value: 'gupshup', label: 'Gupshup' },
  { value: 'wati', label: 'Wati' },
  { value: 'aisensei', label: 'AiSensei' },
  { value: 'interakt', label: 'Interakt' },
  { value: 'meta', label: 'Meta (Cloud API)' },
];

const PROVIDER_FIELDS = {
  gupshup: [
    { key: 'api_key', label: 'API Key', type: 'password' },
    { key: 'app_name', label: 'App Name', type: 'text' },
    { key: 'source_phone', label: 'Source Phone', type: 'text' },
  ],
  wati: [
    { key: 'bearer_token', label: 'Bearer Token', type: 'password' },
    { key: 'tenant_url', label: 'Tenant URL', type: 'text' },
  ],
  aisensei: [
    { key: 'api_key', label: 'API Key', type: 'password' },
    { key: 'base_url', label: 'Base URL', type: 'text' },
  ],
  interakt: [
    { key: 'api_key', label: 'API Key', type: 'password' },
  ],
  meta: [
    { key: 'access_token', label: 'Access Token', type: 'password' },
    { key: 'phone_number_id', label: 'Phone Number ID', type: 'text' },
    { key: 'app_secret', label: 'App Secret', type: 'password' },
    { key: 'verify_token', label: 'Verify Token', type: 'text' },
  ],
};

/* ─── Config Modal ─── */
function ConfigModal({ show, onClose, apiFetch, API_URL, orgProducts, selectedOrg }) {
  const [provider, setProvider] = useState('gupshup');
  const [creds, setCreds] = useState({});
  const [defaultProduct, setDefaultProduct] = useState('');
  const [autoReply, setAutoReply] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    if (!show || !selectedOrg) return;
    apiFetch(`${API_URL}/wa/config`)
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data) {
          setProvider(data.provider || 'gupshup');
          setCreds(data.credentials || {});
          setDefaultProduct(data.default_product_id || '');
          setAutoReply(data.auto_reply !== false);
        }
        setLoaded(true);
      })
      .catch(() => setLoaded(true));
  }, [show, selectedOrg]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await apiFetch(`${API_URL}/wa/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider, credentials: creds, default_product_id: defaultProduct || null, auto_reply: autoReply }),
      });
      onClose();
    } catch (e) { console.error(e); }
    setSaving(false);
  };

  if (!show) return null;

  const fields = PROVIDER_FIELDS[provider] || [];
  const webhookUrl = `https://test.callified.ai/wa/webhook/${provider}`;

  return (
    <div style={overlayStyle} onClick={onClose}>
      <div className="glass-panel" style={modalStyle} onClick={e => e.stopPropagation()}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.2rem' }}>
          <h3 style={{ margin: 0, color: '#e2e8f0' }}>WhatsApp Channel Config</h3>
          <button onClick={onClose} style={closeBtnStyle}>&times;</button>
        </div>

        <label style={labelStyle}>Provider</label>
        <select value={provider} onChange={e => { setProvider(e.target.value); setCreds({}); }} style={inputStyle}>
          {PROVIDERS.map(p => <option key={p.value} value={p.value}>{p.label}</option>)}
        </select>

        {fields.map(f => (
          <div key={f.key}>
            <label style={labelStyle}>{f.label}</label>
            <input type={f.type} value={creds[f.key] || ''} onChange={e => setCreds({ ...creds, [f.key]: e.target.value })}
              style={inputStyle} placeholder={f.label} />
          </div>
        ))}

        <label style={labelStyle}>Default Product</label>
        <select value={defaultProduct} onChange={e => setDefaultProduct(e.target.value)} style={inputStyle}>
          <option value="">— None —</option>
          {(orgProducts || []).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>

        <div style={{ display: 'flex', alignItems: 'center', gap: '10px', margin: '1rem 0' }}>
          <label style={{ ...labelStyle, margin: 0 }}>Auto-Reply</label>
          <button onClick={() => setAutoReply(!autoReply)}
            style={{ ...toggleStyle, background: autoReply ? '#25D366' : '#4a5568' }}>
            {autoReply ? 'ON' : 'OFF'}
          </button>
        </div>

        <div style={{ background: 'rgba(37,211,102,0.08)', border: '1px solid rgba(37,211,102,0.2)', borderRadius: '8px', padding: '0.75rem', marginBottom: '1rem' }}>
          <label style={{ ...labelStyle, fontSize: '0.7rem', color: '#25D366' }}>Webhook URL — configure in your provider dashboard</label>
          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <code style={{ flex: 1, color: '#e2e8f0', fontSize: '0.78rem', wordBreak: 'break-all' }}>{webhookUrl}</code>
            <button onClick={() => navigator.clipboard.writeText(webhookUrl)}
              style={{ ...btnSmallStyle, background: 'rgba(37,211,102,0.15)', color: '#25D366', border: '1px solid rgba(37,211,102,0.3)' }}>
              Copy
            </button>
          </div>
        </div>

        <button onClick={handleSave} disabled={saving}
          style={{ ...btnStyle, width: '100%', background: '#25D366', color: '#fff', fontWeight: 700, opacity: saving ? 0.6 : 1 }}>
          {saving ? 'Saving...' : 'Save Configuration'}
        </button>
      </div>
    </div>
  );
}

/* ─── Main Component ─── */
export default function WhatsAppTab({ apiFetch, API_URL, orgProducts, selectedOrg, orgTimezone }) {
  const [conversations, setConversations] = useState([]);
  const [selectedPhone, setSelectedPhone] = useState(null);
  const [messages, setMessages] = useState([]);
  const [search, setSearch] = useState('');
  const [messageText, setMessageText] = useState('');
  const [sending, setSending] = useState(false);
  const [showConfig, setShowConfig] = useState(false);
  const [aiEnabled, setAiEnabled] = useState({});
  const messagesEndRef = useRef(null);
  const pollRef = useRef(null);

  /* ── Fetch conversations ── */
  const fetchConversations = useCallback(async () => {
    try {
      const res = await apiFetch(`${API_URL}/wa/conversations`);
      if (res.ok) {
        const data = await res.json();
        const convos = Array.isArray(data) ? data : (data.conversations || []);
        setConversations(convos);
        // Build AI-enabled map
        const map = {};
        convos.forEach(c => { map[c.phone || c.contact_phone] = c.ai_active !== false; });
        setAiEnabled(map);
      }
    } catch (e) { console.error('Failed to fetch conversations', e); }
  }, [apiFetch, API_URL]);

  useEffect(() => { fetchConversations(); }, [fetchConversations]);

  /* ── Fetch messages for selected conversation ── */
  const fetchMessages = useCallback(async () => {
    if (!selectedPhone) return;
    try {
      const res = await apiFetch(`${API_URL}/wa/conversations/${encodeURIComponent(selectedPhone)}/messages`);
      if (res.ok) {
          const data = await res.json();
          setMessages(Array.isArray(data) ? data : (data.messages || []));
        }
    } catch (e) { console.error('Failed to fetch messages', e); }
  }, [apiFetch, API_URL, selectedPhone]);

  useEffect(() => { fetchMessages(); }, [fetchMessages]);

  /* ── Poll every 5s ── */
  useEffect(() => {
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = setInterval(() => {
      fetchConversations();
      if (selectedPhone) fetchMessages();
    }, 5000);
    return () => clearInterval(pollRef.current);
  }, [fetchConversations, fetchMessages, selectedPhone]);

  /* ── Auto-scroll ── */
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  /* ── Send message ── */
  const handleSend = async () => {
    if (!messageText.trim() || !selectedPhone || sending) return;
    setSending(true);
    try {
      await apiFetch(`${API_URL}/wa/send`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ contact_phone: selectedPhone, text: messageText }),
      });
      setMessageText('');
      fetchMessages();
      fetchConversations();
    } catch (e) { console.error(e); }
    setSending(false);
  };

  /* ── Toggle AI ── */
  const toggleAi = async () => {
    if (!selectedPhone) return;
    const current = aiEnabled[selectedPhone] !== false;
    try {
      await apiFetch(`${API_URL}/wa/toggle-ai/${encodeURIComponent(selectedPhone)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !current }),
      });
      setAiEnabled(prev => ({ ...prev, [selectedPhone]: !current }));
    } catch (e) { console.error(e); }
  };

  /* ── Filter conversations ── */
  const filtered = conversations.filter(c => {
    if (!search) return true;
    const q = search.toLowerCase();
    return (c.name || '').toLowerCase().includes(q) || (c.phone || '').includes(q);
  });

  const selectedConv = conversations.find(c => c.phone === selectedPhone);
  const aiActive = selectedPhone ? aiEnabled[selectedPhone] !== false : false;

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 140px)', gap: '0' }}>
      {/* ── LEFT PANEL: Conversation List ── */}
      <div className="glass-panel" style={leftPanelStyle}>
        {/* Header */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '0.75rem 1rem', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
          <h3 style={{ margin: 0, color: '#25D366', fontSize: '1rem' }}>
            <span style={{ marginRight: '6px' }}>💬</span>WhatsApp Inbox
          </h3>
          <button onClick={() => setShowConfig(true)}
            style={{ ...btnSmallStyle, background: 'rgba(255,255,255,0.06)', color: '#94a3b8', border: '1px solid rgba(255,255,255,0.1)' }}
            title="Channel Configuration">
            ⚙️
          </button>
        </div>

        {/* Search */}
        <div style={{ padding: '0.5rem 0.75rem' }}>
          <input type="text" placeholder="Search by name or phone..." value={search} onChange={e => setSearch(e.target.value)}
            style={{ ...inputStyle, margin: 0, fontSize: '0.8rem', padding: '6px 10px' }} />
        </div>

        {/* Conversations */}
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {filtered.length === 0 ? (
            <div style={{ padding: '2rem 1rem', textAlign: 'center', color: '#64748b', fontSize: '0.85rem' }}>
              No WhatsApp conversations yet
            </div>
          ) : filtered.map(conv => (
            <div key={conv.phone} onClick={() => setSelectedPhone(conv.phone)}
              style={{
                padding: '0.7rem 1rem', cursor: 'pointer', borderBottom: '1px solid rgba(255,255,255,0.04)',
                background: selectedPhone === conv.phone ? 'rgba(37,211,102,0.08)' : 'transparent',
                transition: 'background 0.15s',
              }}
              onMouseEnter={e => { if (selectedPhone !== conv.phone) e.currentTarget.style.background = 'rgba(255,255,255,0.03)'; }}
              onMouseLeave={e => { if (selectedPhone !== conv.phone) e.currentTarget.style.background = 'transparent'; }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flex: 1, minWidth: 0 }}>
                  {conv.ai_active && <span style={greenDotStyle} title="AI Auto-Reply active" />}
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ color: '#e2e8f0', fontWeight: 600, fontSize: '0.85rem', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {conv.name || conv.phone}
                    </div>
                    <div style={{ fontFamily: 'monospace', color: '#64748b', fontSize: '0.72rem' }}>{conv.phone}</div>
                  </div>
                </div>
                <div style={{ textAlign: 'right', flexShrink: 0, marginLeft: '8px' }}>
                  <div style={{ color: '#64748b', fontSize: '0.68rem' }}>{formatTime(conv.last_message_at, orgTimezone)}</div>
                  {conv.unread_count > 0 && (
                    <span style={unreadBadgeStyle}>{conv.unread_count}</span>
                  )}
                </div>
              </div>
              {conv.last_message && (
                <div style={{ color: '#94a3b8', fontSize: '0.78rem', marginTop: '4px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {conv.last_message.length > 40 ? conv.last_message.substring(0, 40) + '...' : conv.last_message}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      {/* ── RIGHT PANEL: Chat Window ── */}
      <div style={rightPanelStyle}>
        {!selectedPhone ? (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#64748b', fontSize: '0.95rem' }}>
            Select a conversation to start chatting
          </div>
        ) : (
          <>
            {/* Chat Header */}
            <div className="glass-panel" style={chatHeaderStyle}>
              <div style={{ flex: 1 }}>
                <div style={{ color: '#e2e8f0', fontWeight: 700, fontSize: '0.95rem' }}>
                  {selectedConv?.name || selectedPhone}
                </div>
                <div style={{ fontFamily: 'monospace', color: '#64748b', fontSize: '0.78rem' }}>{selectedPhone}</div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <span style={{ color: '#94a3b8', fontSize: '0.78rem' }}>AI Auto-Reply</span>
                <button onClick={toggleAi}
                  style={{
                    ...toggleStyle,
                    background: aiActive ? '#25D366' : '#ef4444',
                    minWidth: '48px',
                  }}>
                  {aiActive ? 'ON' : 'OFF'}
                </button>
              </div>
            </div>

            {/* Messages */}
            <div style={messagesAreaStyle}>
              {messages.length === 0 ? (
                <div style={{ textAlign: 'center', color: '#64748b', marginTop: '3rem', fontSize: '0.85rem' }}>No messages yet</div>
              ) : messages.map((msg, i) => {
                const isOutbound = msg.direction === 'outbound';
                return (
                  <div key={msg.id || i} style={{ display: 'flex', justifyContent: isOutbound ? 'flex-end' : 'flex-start', marginBottom: '8px' }}>
                    <div style={{
                      maxWidth: '70%', padding: '8px 12px', borderRadius: '12px',
                      background: isOutbound ? '#25D366' : '#2d3748',
                      color: isOutbound ? '#fff' : '#e2e8f0',
                      fontSize: '0.85rem', lineHeight: '1.45',
                      borderTopRightRadius: isOutbound ? '4px' : '12px',
                      borderTopLeftRadius: isOutbound ? '12px' : '4px',
                    }}>
                      {msg.ai_generated && <span title="AI-generated" style={{ marginRight: '4px' }}>🤖</span>}
                      <span>{msg.text || msg.body}</span>
                      <div style={{ fontSize: '0.65rem', color: isOutbound ? 'rgba(255,255,255,0.7)' : '#64748b', marginTop: '4px', textAlign: 'right' }}>
                        {formatTime(msg.created_at || msg.timestamp, orgTimezone)}
                      </div>
                    </div>
                  </div>
                );
              })}
              <div ref={messagesEndRef} />
            </div>

            {/* Input Bar */}
            <div style={inputBarStyle}>
              <input type="text" value={messageText}
                onChange={e => setMessageText(e.target.value)}
                onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); handleSend(); } }}
                placeholder="Type a message..."
                style={{ ...inputStyle, flex: 1, margin: 0, fontSize: '0.85rem', padding: '10px 14px' }} />
              <button onClick={handleSend} disabled={sending || !messageText.trim()}
                style={{
                  ...btnStyle, background: '#25D366', color: '#fff', fontWeight: 700,
                  opacity: (sending || !messageText.trim()) ? 0.5 : 1, padding: '10px 20px',
                }}>
                {sending ? '...' : 'Send'}
              </button>
            </div>
          </>
        )}
      </div>

      {/* Config Modal */}
      <ConfigModal show={showConfig} onClose={() => setShowConfig(false)}
        apiFetch={apiFetch} API_URL={API_URL} orgProducts={orgProducts} selectedOrg={selectedOrg} />
    </div>
  );
}

/* ─── Inline Styles ─── */
const leftPanelStyle = {
  width: '30%', minWidth: '280px', display: 'flex', flexDirection: 'column',
  borderRadius: '12px 0 0 12px', overflow: 'hidden',
};

const rightPanelStyle = {
  flex: 1, display: 'flex', flexDirection: 'column',
  background: 'rgba(255,255,255,0.01)', borderRadius: '0 12px 12px 0',
  border: '1px solid rgba(255,255,255,0.06)', borderLeft: 'none',
};

const chatHeaderStyle = {
  display: 'flex', alignItems: 'center', padding: '0.75rem 1rem',
  borderRadius: 0, borderBottom: '1px solid rgba(255,255,255,0.06)',
  margin: 0,
};

const messagesAreaStyle = {
  flex: 1, overflowY: 'auto', padding: '1rem',
  background: 'rgba(0,0,0,0.15)',
};

const inputBarStyle = {
  display: 'flex', gap: '8px', padding: '0.75rem 1rem',
  borderTop: '1px solid rgba(255,255,255,0.06)',
  background: 'rgba(255,255,255,0.02)',
};

const inputStyle = {
  width: '100%', padding: '8px 12px', borderRadius: '8px',
  border: '1px solid rgba(255,255,255,0.1)', background: 'rgba(255,255,255,0.05)',
  color: '#e2e8f0', fontSize: '0.85rem', outline: 'none',
};

const labelStyle = {
  display: 'block', color: '#94a3b8', fontSize: '0.75rem', fontWeight: 600,
  marginBottom: '4px', marginTop: '0.75rem',
};

const btnStyle = {
  border: 'none', borderRadius: '8px', cursor: 'pointer',
  padding: '8px 16px', fontSize: '0.85rem', transition: 'opacity 0.15s',
};

const btnSmallStyle = {
  border: 'none', borderRadius: '6px', cursor: 'pointer',
  padding: '4px 10px', fontSize: '0.75rem',
};

const toggleStyle = {
  border: 'none', borderRadius: '12px', cursor: 'pointer',
  padding: '4px 12px', fontSize: '0.72rem', fontWeight: 700, color: '#fff',
  transition: 'background 0.2s',
};

const greenDotStyle = {
  width: '8px', height: '8px', borderRadius: '50%', background: '#25D366',
  display: 'inline-block', flexShrink: 0,
};

const unreadBadgeStyle = {
  display: 'inline-block', background: '#25D366', color: '#fff',
  borderRadius: '10px', padding: '1px 7px', fontSize: '0.68rem', fontWeight: 700,
  marginTop: '2px',
};

const overlayStyle = {
  position: 'fixed', top: 0, left: 0, right: 0, bottom: 0,
  background: 'rgba(0,0,0,0.6)', display: 'flex', alignItems: 'center', justifyContent: 'center',
  zIndex: 1000,
};

const modalStyle = {
  width: '480px', maxHeight: '85vh', overflowY: 'auto',
  padding: '1.5rem', borderRadius: '12px',
};

const closeBtnStyle = {
  background: 'none', border: 'none', color: '#94a3b8', fontSize: '1.4rem',
  cursor: 'pointer', padding: '0 4px',
};
