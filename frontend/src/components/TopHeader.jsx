import React from 'react';
import { useNavigate, useLocation } from 'react-router-dom';

export default function TopHeader({
  userRole,
  currentUser,
  handleLogout
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const activeTab = location.pathname.replace('/', '') || 'crm';

  return (
    <header className="header" style={{display: 'flex', flexWrap: 'wrap', gap: '1rem', alignItems: 'center'}}>
      <div className="logo" style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
        <img src="https://www.google.com/s2/favicons?domain=globussoft.ai&sz=128" alt="Globussoft Logo" style={{width: '32px', height: '32px', borderRadius: '8px', objectFit: 'contain', background: 'white', padding: '2px'}} />
        Globussoft Generative AI Dialer <span className="badge" style={{background: 'rgba(34, 197, 94, 0.2)', color: '#4ade80', ml: 2}}>LIVE</span>
      </div>

      <div style={{display: 'flex', gap: '10px', alignItems: 'center', flex: 1}}>
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
        {userRole === 'Admin' && <button data-testid="tab-settings" className={`tab-btn ${activeTab === 'settings' ? 'active' : ''}`} onClick={() => navigate('/settings')}>⚙️ Settings</button>}
        {userRole === 'Admin' && <button data-testid="tab-logs" className={`tab-btn ${activeTab === 'logs' ? 'active' : ''}`} onClick={() => navigate('/logs')}>📋 Live Logs</button>}

        <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '12px'}}>
          {currentUser && (
            <span style={{fontSize: '0.8rem', color: '#94a3b8', letterSpacing: '0.5px'}}>
              👤 {currentUser.full_name || currentUser.email} {currentUser.org_name ? `(${currentUser.org_name})` : ''}
            </span>
          )}
          <button data-testid="logout-btn" onClick={handleLogout}
            style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '6px',
              color: '#fca5a5', padding: '6px 14px', cursor: 'pointer', fontWeight: 600, fontSize: '0.8rem'}}>
            🚪 Logout
          </button>
        </div>
      </div>
    </header>
  );
}
