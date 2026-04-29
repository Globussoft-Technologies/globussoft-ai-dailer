import React, { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';

// All Admin-side modules listed in the order they used to render. The first
// PRIMARY_COUNT entries stay inline as tabs; everything after collapses into
// the "More ▾" overflow menu so the nav doesn't horizontally scroll on
// 1400-px laptop screens (issue #36).
const ADMIN_TABS = [
  { key: 'crm',          path: '/crm',          label: '📊 CRM',                testId: 'tab-crm' },
  { key: 'campaigns',    path: '/campaigns',    label: '📢 Campaigns',          testId: 'tab-campaigns' },
  { key: 'ops',          path: '/ops',          label: '📋 Ops & Tasks',        testId: 'tab-ops' },
  { key: 'analytics',    path: '/analytics',    label: '📈 Analytics',          testId: 'tab-analytics' },
  { key: 'whatsapp',     path: '/whatsapp',     label: '💬 WhatsApp Comms',     testId: 'tab-whatsapp' },
  { key: 'integrations', path: '/integrations', label: '🔌 Integrations',       testId: 'tab-integrations' },
  { key: 'monitor',      path: '/monitor',      label: '🎙️ Monitor AI Calls',  testId: 'tab-monitor' },
  { key: 'knowledge',    path: '/knowledge',    label: '🧠 RAG Knowledge',      testId: 'tab-rag' },
  { key: 'sandbox',      path: '/sandbox',      label: '🎯 AI Sandbox',         testId: 'tab-sandbox' },
  { key: 'scheduled',    path: '/scheduled',    label: '📅 Scheduled',          testId: 'tab-scheduled' },
  { key: 'billing',      path: '/billing',      label: '💳 Billing',            testId: 'tab-billing' },
  { key: 'dnd',          path: '/dnd',          label: '🚫 DND',                testId: 'tab-dnd' },
  { key: 'settings',     path: '/settings',     label: '⚙️ Settings',           testId: 'tab-settings' },
  { key: 'logs',         path: '/logs',         label: '📋 Live Logs',          testId: 'tab-logs' },
  { key: 'team',         path: '/team',         label: '👥 Team',               testId: 'tab-team' },
];
const PRIMARY_COUNT = 5;

export default function TopHeader({
  userRole,
  currentUser,
  handleLogout
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const activeTab = location.pathname.replace('/', '') || 'crm';

  const [callingStatus, setCallingStatus] = useState(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const [moreCoords, setMoreCoords] = useState({ top: 0, left: 0 });
  const moreRef = useRef(null);
  const moreBtnRef = useRef(null);
  const morePopupRef = useRef(null);

  useEffect(() => {
    const fetchStatus = () => {
      const token = localStorage.getItem('token');
      if (!token) return;
      fetch('/api/calling-status', { headers: { Authorization: `Bearer ${token}` } })
        .then(r => r.json())
        .then(data => setCallingStatus(data))
        .catch(() => {});
    };
    fetchStatus();
    const interval = setInterval(fetchStatus, 60000);
    return () => clearInterval(interval);
  }, []);

  // Close the More dropdown on outside click or Escape so it doesn't block
  // page interactions (e.g. clicking a campaign card behind it).
  useEffect(() => {
    if (!moreOpen) return;
    const onDocClick = (e) => {
      const inBtn = moreRef.current && moreRef.current.contains(e.target);
      const inPopup = morePopupRef.current && morePopupRef.current.contains(e.target);
      if (!inBtn && !inPopup) setMoreOpen(false);
    };
    const onKey = (e) => { if (e.key === 'Escape') setMoreOpen(false); };
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [moreOpen]);

  // Agents only see CRM; no overflow needed.
  const visibleTabs = userRole === 'Admin' ? ADMIN_TABS : ADMIN_TABS.slice(0, 1);
  const primary = visibleTabs.slice(0, PRIMARY_COUNT);
  const overflow = visibleTabs.slice(PRIMARY_COUNT);
  const overflowHasActive = overflow.some(t => t.key === activeTab);

  const renderTab = (t) => (
    <button
      key={t.key}
      data-testid={t.testId}
      className={`tab-btn ${activeTab === t.key ? 'active' : ''}`}
      onClick={() => navigate(t.path)}
    >
      {t.label}
    </button>
  );

  return (
    <header className="header">
      <div className="logo" style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
        <img src="/logo.png" alt="Globussoft Logo" style={{width: '32px', height: '32px', borderRadius: '8px', objectFit: 'contain'}} />
        Globussoft Generative AI Dialer <span className="badge" style={{background: 'rgba(34, 197, 94, 0.2)', color: '#4ade80', ml: 2}}>LIVE</span>
      </div>

      <div className="tab-bar" style={{display: 'flex', gap: '8px', alignItems: 'center', flex: 1, flexWrap: 'nowrap', minWidth: 0}}>
        {primary.map(renderTab)}

        {overflow.length > 0 && (
          <div ref={moreRef} style={{position: 'relative'}}>
            <button
              type="button"
              ref={moreBtnRef}
              data-testid="tab-more"
              className={`tab-btn ${overflowHasActive ? 'active' : ''}`}
              aria-haspopup="menu"
              aria-expanded={moreOpen}
              onClick={() => {
                // Anchor with position: fixed so the dropdown can never be
                // clipped by an ancestor's overflow/stacking context.
                const rect = moreBtnRef.current?.getBoundingClientRect();
                if (rect) {
                  setMoreCoords({ top: rect.bottom + 6, left: rect.left });
                }
                setMoreOpen(o => !o);
              }}
            >
              More ▾
            </button>
          </div>
        )}
        {moreOpen && overflow.length > 0 && (
          <div role="menu" ref={morePopupRef} style={{
            position: 'fixed', top: moreCoords.top, left: moreCoords.left, zIndex: 9999,
            minWidth: '220px', maxHeight: '70vh', overflowY: 'auto',
            background: 'rgba(15,23,42,0.97)', backdropFilter: 'blur(16px)',
            WebkitBackdropFilter: 'blur(16px)',
            border: '1px solid rgba(255,255,255,0.08)', borderRadius: '10px',
            boxShadow: '0 12px 32px rgba(0,0,0,0.45)', padding: '6px',
          }}>
            {overflow.map(t => (
              <button
                key={t.key}
                type="button"
                role="menuitem"
                data-testid={t.testId}
                onClick={() => { setMoreOpen(false); navigate(t.path); }}
                style={{
                  display: 'block', width: '100%', textAlign: 'left',
                  padding: '8px 12px', borderRadius: '6px',
                  background: activeTab === t.key ? 'rgba(99,102,241,0.18)' : 'transparent',
                  border: 'none',
                  color: activeTab === t.key ? '#c7d2fe' : '#cbd5e1',
                  fontSize: '0.85rem', fontWeight: 600, cursor: 'pointer',
                }}
                onMouseEnter={e => { if (activeTab !== t.key) e.currentTarget.style.background = 'rgba(255,255,255,0.05)'; }}
                onMouseLeave={e => { if (activeTab !== t.key) e.currentTarget.style.background = 'transparent'; }}
              >
                {t.label}
              </button>
            ))}
          </div>
        )}

        <div className="header-user-info" style={{marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '8px', flexShrink: 0}}>
          {callingStatus && (
            <span style={{
              height: '38px',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '6px',
              padding: '0 12px',
              borderRadius: '8px',
              background: callingStatus.allowed ? 'rgba(34, 197, 94, 0.15)' : 'rgba(239, 68, 68, 0.15)',
              border: `1px solid ${callingStatus.allowed ? 'rgba(34, 197, 94, 0.3)' : 'rgba(239, 68, 68, 0.3)'}`,
              color: callingStatus.allowed ? '#4ade80' : '#fca5a5',
              fontWeight: 600,
              fontSize: '0.78rem',
              whiteSpace: 'nowrap',
            }}>
              <span style={{
                width: '7px', height: '7px', borderRadius: '50%',
                background: callingStatus.allowed ? '#22c55e' : '#ef4444',
                flexShrink: 0,
              }} />
              {callingStatus.allowed ? 'Calls Active' : 'Calls Paused'}
            </span>
          )}
          {currentUser && (
            <span style={{
              height: '38px',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '6px',
              padding: '0 12px',
              borderRadius: '8px',
              background: 'rgba(255,255,255,0.04)',
              border: '1px solid rgba(255,255,255,0.08)',
              fontSize: '0.78rem',
              color: '#94a3b8',
              whiteSpace: 'nowrap',
              fontWeight: 600,
            }}>
              👤 {currentUser.full_name || currentUser.email}{currentUser.org_name ? ` (${currentUser.org_name})` : ''}
            </span>
          )}
          <button data-testid="logout-btn" onClick={handleLogout}
            style={{
              height: '38px',
              display: 'inline-flex',
              alignItems: 'center',
              gap: '5px',
              padding: '0 14px',
              background: 'rgba(239,68,68,0.15)',
              border: '1px solid rgba(239,68,68,0.3)',
              borderRadius: '8px',
              color: '#fca5a5',
              cursor: 'pointer',
              fontWeight: 600,
              fontSize: '0.82rem',
              whiteSpace: 'nowrap',
            }}>
            🚪 Logout
          </button>
        </div>
      </div>
    </header>
  );
}
