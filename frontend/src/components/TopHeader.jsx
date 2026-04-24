import React, { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';

export default function TopHeader({
  userRole,
  currentUser,
  handleLogout
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const activeTab = location.pathname.replace('/', '') || 'crm';

  const [callingStatus, setCallingStatus] = useState(null);

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

  return (
    <header className="header">
      <div className="logo" style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
        <img src="/logo.png" alt="Globussoft Logo" style={{width: '32px', height: '32px', borderRadius: '8px', objectFit: 'contain'}} />
        Globussoft Generative AI Dialer <span className="badge" style={{background: 'rgba(34, 197, 94, 0.2)', color: '#4ade80', ml: 2}}>LIVE</span>
      </div>

      <div className="tab-bar" style={{display: 'flex', gap: '8px', alignItems: 'center', flex: 1, flexWrap: 'nowrap'}}>
        <button data-testid="tab-crm" className={`tab-btn ${activeTab === 'crm' ? 'active' : ''}`} onClick={() => navigate('/crm')}>📊 CRM</button>
        {userRole === 'Admin' && <button data-testid="tab-campaigns" className={`tab-btn ${activeTab === 'campaigns' ? 'active' : ''}`} onClick={() => navigate('/campaigns')}>📢 Campaigns</button>}
        {userRole === 'Admin' && <button data-testid="tab-ops" className={`tab-btn ${activeTab === 'ops' ? 'active' : ''}`} onClick={() => navigate('/ops')}>📋 Ops & Tasks</button>}
        {userRole === 'Admin' && <button data-testid="tab-analytics" className={`tab-btn ${activeTab === 'analytics' ? 'active' : ''}`} onClick={() => navigate('/analytics')}>📈 Analytics</button>}
        {userRole === 'Admin' && <button data-testid="tab-whatsapp" className={`tab-btn ${activeTab === 'whatsapp' ? 'active' : ''}`} onClick={() => navigate('/whatsapp')}>💬 WhatsApp Comms</button>}
        {userRole === 'Admin' && <button data-testid="tab-integrations" className={`tab-btn ${activeTab === 'integrations' ? 'active' : ''}`} onClick={() => navigate('/integrations')}>🔌 Integrations</button>}
        {userRole === 'Admin' && <button data-testid="tab-monitor" className={`tab-btn ${activeTab === 'monitor' ? 'active' : ''}`} onClick={() => navigate('/monitor')}>🎙️ Monitor AI Calls</button>}
        {userRole === 'Admin' && <button data-testid="tab-rag" className={`tab-btn ${activeTab === 'knowledge' ? 'active' : ''}`} onClick={() => navigate('/knowledge')}>🧠 RAG Knowledge</button>}
        {userRole === 'Admin' && <button data-testid="tab-sandbox" className={`tab-btn ${activeTab === 'sandbox' ? 'active' : ''}`} onClick={() => navigate('/sandbox')}>🎯 AI Sandbox</button>}
        {userRole === 'Admin' && <button data-testid="tab-scheduled" className={`tab-btn ${activeTab === 'scheduled' ? 'active' : ''}`} onClick={() => navigate('/scheduled')}>📅 Scheduled</button>}
        {userRole === 'Admin' && <button data-testid="tab-billing" className={`tab-btn ${activeTab === 'billing' ? 'active' : ''}`} onClick={() => navigate('/billing')}>💳 Billing</button>}
        {userRole === 'Admin' && <button data-testid="tab-dnd" className={`tab-btn ${activeTab === 'dnd' ? 'active' : ''}`} onClick={() => navigate('/dnd')}>🚫 DND</button>}
        {userRole === 'Admin' && <button data-testid="tab-settings" className={`tab-btn ${activeTab === 'settings' ? 'active' : ''}`} onClick={() => navigate('/settings')}>⚙️ Settings</button>}
        {userRole === 'Admin' && <button data-testid="tab-logs" className={`tab-btn ${activeTab === 'logs' ? 'active' : ''}`} onClick={() => navigate('/logs')}>📋 Live Logs</button>}
        {userRole === 'Admin' && <button data-testid="tab-team" className={`tab-btn ${activeTab === 'team' ? 'active' : ''}`} onClick={() => navigate('/team')}>👥 Team</button>}

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
