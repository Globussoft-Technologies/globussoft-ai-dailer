import React, { useState, useEffect } from 'react';
import './index.css';

const API_URL = "http://localhost:8000/api";

export default function App() {
  const [activeTab, setActiveTab] = useState('crm');
  const [leads, setLeads] = useState([]);
  const [sites, setSites] = useState([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [dialingId, setDialingId] = useState(null);
  
  const [formData, setFormData] = useState({ first_name: '', last_name: '', phone: '', source: 'Manual Entry' });

  const [fieldOpsData, setFieldOpsData] = useState({ agent_name: '', site_id: '' });
  const [punchStatus, setPunchStatus] = useState(null);
  const [punching, setPunching] = useState(false);

  // Workflow State
  const [tasks, setTasks] = useState([]);
  const [reports, setReports] = useState(null);

  // WhatsApp State
  const [whatsappLogs, setWhatsappLogs] = useState([]);

  // Document Vault State
  const [activeLeadDocs, setActiveLeadDocs] = useState(null);
  const [docs, setDocs] = useState([]);
  const [docFormData, setDocFormData] = useState({ file_name: '', file_url: '' });

  // Analytics State
  const [analyticsData, setAnalyticsData] = useState([]);

  // Search Engine State
  const [searchQuery, setSearchQuery] = useState('');
  
  // Integrations State
  const [integrations, setIntegrations] = useState([]);
  const CRM_SCHEMAS = {
    "Salesforce": [{ key: "client_id", label: "OAuth Client ID", type: "text" }, { key: "client_secret", label: "OAuth Client Secret", type: "password" }, { key: "instance_url", label: "Instance Base URL", type: "text" }],
    "HubSpot": [{ key: "api_key", label: "Private App Access Token", type: "password" }],
    "Zoho CRM": [{ key: "client_id", label: "Client ID", type: "text" }, { key: "client_secret", label: "Client Secret", type: "password" }, { key: "refresh_token", label: "OAuth Refresh Token", type: "password" }, { key: "base_url", label: "Data Center (e.g. www.zohoapis.com)", type: "text" }],
    "Pipedrive": [{ key: "api_key", label: "Personal API Token", type: "password" }],
    "ActiveCampaign": [{ key: "api_key", label: "Developer API Token", type: "password" }, { key: "base_url", label: "Account URL (https://xyz.api-us1.com/api/3)", type: "text" }],
    "Freshsales": [{ key: "api_key", label: "API Token", type: "password" }, { key: "base_url", label: "Bundle URL (https://domain.myfreshworks.com/crm/sales/api)", type: "text" }],
    "Zendesk": [{ key: "api_key", label: "API Token or Password", type: "password" }, { key: "base_url", label: "Subdomain Base URL", type: "text" }, { key: "email", label: "Admin Email (If Basic Auth)", type: "text" }],
    "Monday": [{ key: "api_key", label: "Personal API Token", type: "password" }, { key: "board_id", label: "Leads Board ID", type: "text" }],
    "Close": [{ key: "api_key", label: "API Key", type: "password" }]
  };
  const [intFormData, setIntFormData] = useState({ provider: 'HubSpot', credentials: {} });

  // RBAC Global State
  const [userRole, setUserRole] = useState('Admin'); // 'Admin' or 'Agent'

  // GenAI Email Modal State
  const [emailDraft, setEmailDraft] = useState(null);

  const fetchLeads = async () => {
    try {
      const res = await fetch(`${API_URL}/leads`);
      const data = await res.json();
      setLeads(data);
    } catch (e) {
      console.error("Make sure FastAPI is running with CORS enabled!", e);
    }
  };

  const fetchSites = async () => {
    try {
      const res = await fetch(`${API_URL}/sites`);
      setSites(await res.json());
    } catch (e) {
      console.error("Could not fetch sites:", e);
    }
  };

  const fetchTasks = async () => {
    try { const res = await fetch(`${API_URL}/tasks`); setTasks(await res.json()); } catch(e){}
  };

  const fetchReports = async () => {
    try { const res = await fetch(`${API_URL}/reports`); setReports(await res.json()); } catch(e){}
  };

  const fetchWhatsappLogs = async () => {
    try { const res = await fetch(`${API_URL}/whatsapp`); setWhatsappLogs(await res.json()); } catch(e){}
  };

  const fetchAnalytics = async () => {
    try { const res = await fetch(`${API_URL}/analytics`); setAnalyticsData(await res.json()); } catch(e){}
  };

  const fetchIntegrations = async () => {
    try { const res = await fetch(`${API_URL}/integrations`); setIntegrations(await res.json()); } catch(e){}
  };

  useEffect(() => {
    fetchLeads();
    fetchSites();
    fetchTasks();
    fetchReports();
    fetchWhatsappLogs();
    fetchAnalytics();
    fetchIntegrations();
  }, []);

  const handleStatusChange = async (leadId, newStatus) => {
    try {
      await fetch(`${API_URL}/leads/${leadId}/status`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: newStatus })
      });
      fetchLeads();
      fetchTasks();
      fetchReports();
      fetchWhatsappLogs();
    } catch (e) { console.error(e); }
  };

  const handleCompleteTask = async (taskId) => {
    try {
      await fetch(`${API_URL}/tasks/${taskId}/complete`, { method: 'PUT' });
      fetchTasks();
      fetchReports();
    } catch (e) { console.error(e); }
  };

  const handleCreateLead = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await fetch(`${API_URL}/leads`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(formData)
      });
      setFormData({ first_name: '', last_name: '', phone: '', source: 'Manual Entry' });
      setIsModalOpen(false);
      fetchLeads();
    } catch(e) {
      console.error(e);
    }
    setLoading(false);
  };

  const handleDial = async (lead) => {
    setDialingId(lead.id);
    try {
      const res = await fetch(`${API_URL}/dial/${lead.id}`, { method: "POST" });
      const data = await res.json();
      alert(`Status: ${data.message || 'Connecting call...'}`);
    } catch(e) {
      alert("Failed to hit the dialer API. Check console.");
    }
    setTimeout(() => setDialingId(null), 3000);
  };

  const handleOpenDocs = async (lead) => {
    setActiveLeadDocs(lead);
    try {
      const res = await fetch(`${API_URL}/leads/${lead.id}/documents`);
      setDocs(await res.json());
    } catch(e) {}
  };

  const handleUploadDoc = async (e) => {
    e.preventDefault();
    try {
      await fetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(docFormData)
      });
      setDocFormData({ file_name: '', file_url: '' });
      const res = await fetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`);
      setDocs(await res.json());
    } catch(e) { console.error(e); }
  };

  const handlePunchIn = () => {
    if (!fieldOpsData.agent_name || !fieldOpsData.site_id) {
      alert("Please enter your name and select a site.");
      return;
    }
    setPunching(true);
    setPunchStatus(null);
    if (!navigator.geolocation) {
      alert("Geolocation is not supported by your browser");
      setPunching(false);
      return;
    }
    navigator.geolocation.getCurrentPosition(async (position) => {
      try {
        const response = await fetch(`${API_URL}/punch`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            agent_name: fieldOpsData.agent_name,
            site_id: parseInt(fieldOpsData.site_id),
            lat: position.coords.latitude,
            lon: position.coords.longitude
          })
        });
        const data = await response.json();
        setPunchStatus(data);
        fetchReports();
      } catch (e) {
        setPunchStatus({ status: 'error', message: 'Network error checking in.' });
      } finally {
        setPunching(false);
      }
    }, (error) => {
      alert(`Error fetching location: ${error.message}`);
      setPunching(false);
    });
  };

  const handleSearch = async (e) => {
    const query = e.target.value;
    setSearchQuery(query);
    if (query.trim().length >= 2) {
      try {
        const res = await fetch(`${API_URL}/leads/search?q=${encodeURIComponent(query)}`);
        setLeads(await res.json());
      } catch(e) {}
    } else if (query.trim().length === 0) {
      fetchLeads();
    }
  };

  const handleNote = async (lead) => {
    const rawNote = lead.follow_up_note || '';
    const newNote = prompt(`Update the manual timeline note for ${lead.first_name} ${lead.last_name}:`, rawNote);
    if (newNote !== null) {
      try {
        await fetch(`${API_URL}/leads/${lead.id}/notes`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ note: newNote })
        });
        fetchLeads(); // Instantly refresh UI
      } catch(e) {
        console.error("Error saving note", e);
      }
    }
  };

  const handleDraftEmail = async (lead) => {
    setDialingId(lead.id); // Reuse the dialing spinner temporarily
    try {
      const res = await fetch(`${API_URL}/leads/${lead.id}/draft-email`);
      const data = await res.json();
      setEmailDraft(data);
    } catch(e) {
      console.error("Error generating email", e);
    }
    setDialingId(null);
  };

  const handleCreateIntegration = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await fetch(`${API_URL}/integrations`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          provider: intFormData.provider,
          credentials: intFormData.credentials
        })
      });
      setIntFormData({ provider: 'HubSpot', credentials: {} });
      fetchIntegrations();
      alert("Integration saved successfully!");
    } catch(e) {
      console.error(e);
      alert("Failed to save integration.");
    }
    setLoading(false);
  };

  return (
    <div className="dashboard-container">
      <header className="header" style={{display: 'flex', flexWrap: 'wrap', gap: '1rem', alignItems: 'center'}}>
        <div className="logo" style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
          <img src="https://www.google.com/s2/favicons?domain=globussoft.ai&sz=128" alt="Globussoft Logo" style={{width: '32px', height: '32px', borderRadius: '8px', objectFit: 'contain', background: 'white', padding: '2px'}} />
          Globussoft Generative AI Dialer <span className="badge" style={{background: 'rgba(34, 197, 94, 0.2)', color: '#4ade80', ml: 2}}>LIVE</span>
        </div>
        
        <div style={{display: 'flex', gap: '10px', alignItems: 'center', flex: 1}}>
          <button className={`tab-btn ${activeTab === 'crm' ? 'active' : ''}`} onClick={() => setActiveTab('crm')}>📊 CRM</button>
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'ops' ? 'active' : ''}`} onClick={() => setActiveTab('ops')}>📋 Ops & Tasks</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'analytics' ? 'active' : ''}`} onClick={() => setActiveTab('analytics')}>📈 Analytics</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'whatsapp' ? 'active' : ''}`} onClick={() => setActiveTab('whatsapp')}>💬 WhatsApp Comms</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'fieldops' ? 'active' : ''}`} onClick={() => setActiveTab('fieldops')}>📍 Field Ops</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'integrations' ? 'active' : ''}`} onClick={() => setActiveTab('integrations')}>🔌 Integrations</button>}
          
          {/* RBAC Global Toggle */}
          <div className="role-selector" style={{marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '10px'}}>
            <span style={{fontSize: '0.8rem', color: '#94a3b8', textTransform: 'uppercase', letterSpacing: '1px'}}>👤 View As:</span>
            <select 
              value={userRole} 
              onChange={(e) => {
                setUserRole(e.target.value);
                if (e.target.value === 'Agent' && activeTab !== 'crm') {
                  setActiveTab('crm');
                }
              }}
              style={{background: 'rgba(0,0,0,0.3)', color: userRole === 'Admin' ? '#f59e0b' : '#38bdf8', border: '1px solid rgba(255,255,255,0.1)', padding: '6px 12px', borderRadius: '4px', fontWeight: 'bold'}}
            >
              <option value="Admin">Admin</option>
              <option value="Agent">BDR Agent</option>
            </select>
          </div>
        </div>
      </header>
      
      {activeTab === 'crm' ? (
        <div className="crm-container">
          <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', marginBottom: '1rem'}}>
            <h2 style={{marginTop: 0, marginBottom: 0}}>Deal Pipeline</h2>
            <div style={{position: 'relative'}}>
              <input 
                type="text" 
                className="form-input" 
                placeholder="🔍 Search Leads by Name or Phone..." 
                value={searchQuery}
                onChange={handleSearch}
                style={{width: '320px', borderRadius: '30px', paddingLeft: '20px', marginBottom: 0, background: 'rgba(15, 23, 42, 0.6)'}}
              />
            </div>

            <div style={{display: 'flex', gap: '10px', marginLeft: '1rem'}}>
              <button className="btn-primary" onClick={() => setIsModalOpen(true)}>
                + Add Lead
              </button>
              {userRole === 'Admin' && (
                <button className="btn-call" style={{borderColor: '#22c55e', color: '#22c55e', padding: '0 16px', height: '40px', background: 'rgba(34, 197, 94, 0.1)', cursor: 'pointer'}} onClick={() => window.open(`${API_URL}/leads/export`, '_blank')}>
                  📥 Export CSV
                </button>
              )}
            </div>
          </div>

          {userRole === 'Admin' && (
            <div className="metrics-grid">
              <div className="glass-panel metric-card">
                <div className="metric-label">Total Leads</div>
                <div className="metric-value">{leads.length}</div>
              </div>
              <div className="glass-panel metric-card">
                <div className="metric-label">Active Calls</div>
                <div className="metric-value">0</div>
              </div>
              <div className="glass-panel metric-card">
                <div className="metric-label">Success Rate</div>
                <div className="metric-value">94%</div>
              </div>
            </div>
          )}

          <div className="glass-panel" style={{overflowX: 'auto'}}>
            <h2 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.25rem', fontWeight: 600}}>Campaign Leads</h2>
            <table className="leads-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Phone</th>
                  <th>Source</th>
                  <th>Status</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                {leads.length === 0 ? (
                  <tr><td colSpan="5" style={{textAlign: "center", padding: "3rem", color: '#94a3b8'}}>No leads found. Click 'Add Lead' to populate!</td></tr>
                ) : leads.map(lead => (
                  <React.Fragment key={lead.id}>
                    <tr>
                      <td style={{fontWeight: 500}}>{lead.first_name} {lead.last_name}</td>
                      <td style={{fontFamily: 'SFMono-Regular, Consolas, monospace', color: '#cbd5e1'}}>{lead.phone}</td>
                      <td><span className="badge">{lead.source}</span></td>
                      <td>
                        <select 
                          value={lead.status || 'new'} 
                          onChange={(e) => handleStatusChange(lead.id, e.target.value)}
                          style={{background: 'rgba(0,0,0,0.3)', color: '#fff', border: '1px solid rgba(255,255,255,0.1)', padding: '4px 8px', borderRadius: '4px'}}
                        >
                          <option value="new">New</option>
                          <option value="Warm">Warm</option>
                          <option value="Summarized">Summarized</option>
                          <option value="Closed">Closed</option>
                        </select>
                      </td>
                      <td>
                        <div style={{display: 'flex', gap: '8px'}}>
                          <button 
                            className="btn-call" 
                            style={{background: 'rgba(56, 189, 248, 0.15)', color: '#38bdf8', borderColor: 'rgba(56, 189, 248, 0.3)'}}
                            onClick={() => handleOpenDocs(lead)}
                          >
                            📁 Docs
                          </button>
                          <button 
                            className="btn-call" 
                            style={{background: 'rgba(168, 85, 247, 0.15)', color: '#a855f7', borderColor: 'rgba(168, 85, 247, 0.3)'}}
                            onClick={() => handleNote(lead)}
                          >
                            📝 Note
                          </button>
                          <button 
                            className="btn-call" 
                            style={{background: 'linear-gradient(135deg, rgba(245, 158, 11, 0.15), rgba(220, 38, 38, 0.15))', color: '#f59e0b', borderColor: 'rgba(245, 158, 11, 0.3)'}}
                            onClick={() => handleDraftEmail(lead)}
                            disabled={dialingId === lead.id}
                          >
                            {dialingId === lead.id ? 'Thinking...' : '📧 AI Email'}
                          </button>
                          <button 
                            className="btn-call" 
                            onClick={() => handleDial(lead)}
                            disabled={dialingId === lead.id}
                          >
                            {dialingId === lead.id ? 'Dialing...' : '📞 Call'}
                          </button>
                        </div>
                      </td>
                    </tr>
                    {lead.follow_up_note && (
                      <tr>
                        <td colSpan="5" style={{padding: '16px 24px', background: 'rgba(0,0,0,0.2)', borderLeft: '3px solid #6366f1'}}>
                          <div style={{fontSize: '0.85rem', color: '#94a3b8', marginBottom: '8px', textTransform: 'uppercase', letterSpacing: '1px', fontWeight: 600}}>AI Follow-Up Note</div>
                          <div style={{whiteSpace: 'pre-wrap', color: '#e2e8f0', fontSize: '0.9rem', lineHeight: 1.6}}>{lead.follow_up_note}</div>
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : activeTab === 'ops' ? (
        <div className="ops-container" style={{padding: '1rem'}}>
          {reports && (
            <div className="metrics-grid" style={{marginBottom: '3rem'}}>
              <div className="glass-panel metric-card" style={{padding: '1.2rem'}}>
                <div className="metric-label">Closed Deals</div>
                <div className="metric-value" style={{color: '#34d399'}}>{reports.closed_deals}</div>
              </div>
              <div className="glass-panel metric-card" style={{padding: '1.2rem'}}>
                <div className="metric-label">Verified Punches</div>
                <div className="metric-value">{reports.valid_site_punches}</div>
              </div>
              <div className="glass-panel metric-card" style={{padding: '1.2rem'}}>
                <div className="metric-label">Pending Tasks</div>
                <div className="metric-value" style={{color: '#fbbf24'}}>{reports.pending_internal_tasks}</div>
              </div>
            </div>
          )}

          <div className="glass-panel">
            <h2 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.25rem', fontWeight: 600}}>Internal Cross-Department Tasks</h2>
            <div className="task-list">
              {tasks.length === 0 ? (
                <p style={{color: '#94a3b8', textAlign: 'center'}}>No internal workflows active. Try closing a lead in CRM!</p>
              ) : tasks.map(t => (
                <div key={t.id} style={{
                  display: 'flex', justifyContent: 'space-between', alignItems: 'center',
                  background: 'rgba(255,255,255,0.03)', padding: '16px', borderRadius: '8px', marginBottom: '12px',
                  borderLeft: t.status === 'Complete' ? '4px solid #34d399' : '4px solid #fbbf24'
                }}>
                  <div>
                    <div style={{display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px'}}>
                      <span className="badge" style={{background: 'rgba(255,255,255,0.1)', color: '#fff', border: 'none'}}>{t.department}</span>
                      <span style={{fontSize: '0.9rem', color: '#cbd5e1'}}>Client: {t.first_name} {t.last_name}</span>
                    </div>
                    <p style={{margin: 0, color: t.status === 'Complete' ? '#94a3b8' : '#f8fafc', textDecoration: t.status === 'Complete' ? 'line-through' : 'none'}}>
                      {t.description}
                    </p>
                  </div>
                  <div>
                    {t.status === 'Complete' ? (
                      <span style={{color: '#34d399', fontWeight: 600, fontSize: '0.9rem'}}>✓ Done</span>
                    ) : (
                      <button className="btn-call" onClick={() => handleCompleteTask(t.id)}>Mark Done</button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : activeTab === 'analytics' ? (
        <div className="analytics-container">
          <div className="wa-header" style={{borderBottom: '1px solid rgba(255,255,255,0.05)', marginBottom: '2rem'}}>
            <h3><span style={{color: '#f59e0b'}}>Executive</span> Data Analytics</h3>
            <p>7-Day trailing performance. Real-time insights derived from CRM pipelines.</p>
          </div>
          
          <div style={{display: 'flex', gap: '2rem', padding: '0 24px'}}>
            <div className="glass-panel" style={{flex: 1}}>
              <h4 style={{marginTop: 0, color: '#94a3b8', fontSize: '0.9rem', textTransform: 'uppercase', letterSpacing: '1px'}}>Call Volume vs. Deals Closed</h4>
              
              <div className="chart-wrapper">
                {analyticsData.map((stat, i) => {
                  const maxCalls = Math.max(...analyticsData.map(d => d.calls)) || 100;
                  const callHeight = Math.max(5, (stat.calls / maxCalls) * 100);
                  const closedHeight = Math.max(2, (stat.closed / maxCalls) * 100 * 5); // Exaggerated slightly to be visible next to calls

                  return (
                    <div className="bar-group" key={i}>
                      <div className="bar calls-bar" style={{height: `${callHeight}%`}}>
                        <div className="tooltip">{stat.calls} Calls</div>
                      </div>
                      <div className="bar closed-bar" style={{height: `${closedHeight}%`}}>
                        <div className="tooltip">{stat.closed} Closed</div>
                      </div>
                      <div className="bar-label">
                        {stat.day}<br/>
                        <span style={{fontSize: '0.7rem', color: '#64748b'}}>{stat.date}</span>
                      </div>
                    </div>
                  );
                })}
              </div>
              <div style={{display: 'flex', justifyContent: 'center', gap: '2rem', marginTop: '1rem'}}>
                <div style={{display: 'flex', alignItems: 'center', gap: '8px', fontSize: '0.85rem'}}><div style={{width: '12px', height: '12px', background: 'var(--primary)', borderRadius: '2px'}}></div> Total Calls</div>
                <div style={{display: 'flex', alignItems: 'center', gap: '8px', fontSize: '0.85rem'}}><div style={{width: '12px', height: '12px', background: '#22c55e', borderRadius: '2px'}}></div> Won Deals</div>
              </div>
            </div>
          </div>
        </div>
      ) : activeTab === 'whatsapp' ? (
        <div className="whatsapp-container">
          <div className="wa-header">
            <h3><span style={{color: '#25D366'}}>WhatsApp</span> Outbound Automated Logs</h3>
            <p>Monitors triggered property e-brochures and automated conversational nudges.</p>
          </div>
          <div className="wa-chat-window">
            {whatsappLogs.length === 0 ? (
              <div className="wa-empty">No WhatsApp triggers sent yet. Change a Lead Status to "Warm" in CRM!</div>
            ) : whatsappLogs.map(log => (
              <div key={log.id} className="wa-message-row">
                <div className="wa-message-bubble">
                  <div className="wa-message-recipient">To: {log.first_name} {log.last_name} ({log.phone})</div>
                  <div className="wa-message-body">{log.message}</div>
                  <div className="wa-message-meta">
                    <span className="wa-pill">{log.msg_type} Trigger</span>
                    <span className="wa-time">{new Date(log.sent_at).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}</span>
                    <span className="wa-ticks">✓✓</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : activeTab === 'integrations' ? (
        <div className="integrations-container" style={{padding: '1rem'}}>
          <div className="wa-header" style={{borderBottom: '1px solid rgba(255,255,255,0.05)', marginBottom: '2rem'}}>
            <h3><span style={{color: '#38bdf8'}}>CRM</span> Integrations</h3>
            <p>Connect external CRM platforms to pull leads automatically and push call outcomes back.</p>
          </div>
          
          <div style={{display: 'grid', gridTemplateColumns: 'minmax(300px, 400px) 1fr', gap: '2rem'}}>
            <div className="glass-panel" style={{height: 'fit-content'}}>
              <h4 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.1rem', fontWeight: 600}}>Add New Connection</h4>
              <form onSubmit={handleCreateIntegration} style={{display: 'flex', flexDirection: 'column', gap: '1rem'}}>
                <div className="form-group" style={{marginBottom: 0}}>
                  <label>Provider</label>
                  <select className="form-input" value={intFormData.provider} onChange={e => setIntFormData({provider: e.target.value, credentials: {}})}>
                    <option value="HubSpot">HubSpot</option>
                    <option value="Salesforce">Salesforce</option>
                    <option value="Zoho">Zoho CRM</option>
                    <option value="Pipedrive">Pipedrive</option>
                    <option value="ActiveCampaign">ActiveCampaign</option>
                    <option value="Freshsales">Freshsales</option>
                    <option value="Monday">Monday</option>
                    <option value="Keap">Keap</option>
                    <option value="Zendesk">Zendesk</option>
                    <option value="Bitrix24">Bitrix24</option>
                    <option value="Insightly">Insightly</option>
                    <option value="Copper">Copper</option>
                    <option value="Nimble">Nimble</option>
                    <option value="Nutshell">Nutshell</option>
                    <option value="Capsule">Capsule</option>
                    <option value="AgileCRM">AgileCRM</option>
                    <option value="SugarCRM">SugarCRM</option>
                    <option value="Vtiger">Vtiger</option>
                    <option value="Apptivo">Apptivo</option>
                    <option value="Creatio">Creatio</option>
                    <option value="Maximizer">Maximizer</option>
                    <option value="Salesflare">Salesflare</option>
                    <option value="Close">Close</option>
                    <option value="Pipeline">Pipeline</option>
                    <option value="ReallySimpleSystems">ReallySimpleSystems</option>
                    <option value="EngageBay">EngageBay</option>
                    <option value="Ontraport">Ontraport</option>
                    <option value="Kustomer">Kustomer</option>
                    <option value="Dynamics365">Dynamics365</option>
                    <option value="OracleCX">OracleCX</option>
                    <option value="SAPCRM">SAPCRM</option>
                    <option value="NetSuite">NetSuite</option>
                    <option value="SageCRM">SageCRM</option>
                    <option value="Pegasystems">Pegasystems</option>
                    <option value="InforCRM">InforCRM</option>
                    <option value="Workbooks">Workbooks</option>
                    <option value="Kintone">Kintone</option>
                    <option value="Scoro">Scoro</option>
                    <option value="Odoo">Odoo</option>
                    <option value="Streak">Streak</option>
                    <option value="LessAnnoyingCRM">LessAnnoyingCRM</option>
                    <option value="Daylite">Daylite</option>
                    <option value="ConvergeHub">ConvergeHub</option>
                    <option value="Claritysoft">Claritysoft</option>
                    <option value="AmoCRM">AmoCRM</option>
                    <option value="BenchmarkONE">BenchmarkONE</option>
                    <option value="Bigin">Bigin</option>
                    <option value="BoomTown">BoomTown</option>
                    <option value="BuddyCRM">BuddyCRM</option>
                    <option value="Bullhorn">Bullhorn</option>
                    <option value="CiviCRM">CiviCRM</option>
                    <option value="ClientLook">ClientLook</option>
                    <option value="ClientSuccess">ClientSuccess</option>
                    <option value="ClientTether">ClientTether</option>
                    <option value="CommandCenter">CommandCenter</option>
                    <option value="ConnectWise">ConnectWise</option>
                    <option value="Contactually">Contactually</option>
                    <option value="Corezoid">Corezoid</option>
                    <option value="CRMNext">CRMNext</option>
                    <option value="Daycos">Daycos</option>
                    <option value="DealerSocket">DealerSocket</option>
                    <option value="Efficy">Efficy</option>
                    <option value="Enquire">Enquire</option>
                    <option value="Entrata">Entrata</option>
                    <option value="Epsilon">Epsilon</option>
                    <option value="EspoCRM">EspoCRM</option>
                    <option value="Exact">Exact</option>
                    <option value="Flowlu">Flowlu</option>
                    <option value="FollowUpBoss">FollowUpBoss</option>
                    <option value="Front">Front</option>
                    <option value="Funnel">Funnel</option>
                    <option value="Genesis">Genesis</option>
                    <option value="GoHighLevel">GoHighLevel</option>
                    <option value="GoldMine">GoldMine</option>
                    <option value="GreenRope">GreenRope</option>
                    <option value="Highrise">Highrise</option>
                    <option value="iContact">iContact</option>
                    <option value="Infusionsoft">Infusionsoft</option>
                    <option value="IxactContact">IxactContact</option>
                    <option value="Jobber">Jobber</option>
                    <option value="Junxure">Junxure</option>
                    <option value="Kaseya">Kaseya</option>
                    <option value="Kixie">Kixie</option>
                    <option value="Klaviyo">Klaviyo</option>
                    <option value="Kommo">Kommo</option>
                    <option value="LeadSquared">LeadSquared</option>
                    <option value="LionDesk">LionDesk</option>
                    <option value="Lusha">Lusha</option>
                    <option value="Mailchimp">Mailchimp</option>
                    <option value="Marketo">Marketo</option>
                    <option value="Membrain">Membrain</option>
                    <option value="MethodCRM">MethodCRM</option>
                    <option value="MightyCRM">MightyCRM</option>
                    <option value="Mindbody">Mindbody</option>
                    <option value="Mixpanel">Mixpanel</option>
                    <option value="Navatar">Navatar</option>
                    <option value="NetHunt">NetHunt</option>
                    <option value="NexTravel">NexTravel</option>
                    <option value="Nurture">Nurture</option>
                    <option value="OnePageCRM">OnePageCRM</option>
                    <option value="Pipeliner">Pipeliner</option>
                    <option value="Planhat">Planhat</option>
                    <option value="Podio">Podio</option>

                  </select>
                </div>
                {(CRM_SCHEMAS[intFormData.provider] || [{ key: 'api_key', label: 'API Key / Token', type: 'password' }, { key: 'base_url', label: 'REST API Base URL', type: 'text' }]).map(field => (
                  <div className="form-group" key={field.key} style={{marginBottom: 0}}>
                    <label>{field.label}</label>
                    <input 
                      type={field.type} 
                      className="form-input" 
                      value={intFormData.credentials[field.key] || ''} 
                      onChange={e => setIntFormData({...intFormData, credentials: {...intFormData.credentials, [field.key]: e.target.value}})} 
                      placeholder={field.label + "..."} 
                    />
                  </div>
                ))}
                <button type="submit" className="btn-primary" disabled={loading} style={{marginTop: '0.5rem'}}>
                  {loading ? 'Connecting...' : '🔌 Save Connection'}
                </button>
              </form>
            </div>

            <div className="glass-panel" style={{overflowX: 'auto'}}>
              <h4 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.1rem', fontWeight: 600}}>Active Connections</h4>
              <table className="leads-table">
                <thead>
                  <tr>
                    <th>Provider</th>
                    <th>API Key (Masked)</th>
                    <th>Status</th>
                    <th>Last Synced</th>
                  </tr>
                </thead>
                <tbody>
                  {integrations.length === 0 ? (
                     <tr><td colSpan="4" style={{textAlign: "center", padding: "2rem", color: '#94a3b8'}}>No implementations hooked yet.</td></tr>
                  ) : integrations.map(intg => (
                    <tr key={intg.id}>
                      <td style={{fontWeight: 'bold', color: '#e2e8f0'}}>{intg.provider}</td>
                      <td style={{fontFamily: 'monospace', color: '#cbd5e1', fontSize: '0.85rem'}}>
                         {Object.keys(intg.credentials || {}).map(k => (
                            <div key={k}>{k}: ****</div>
                         ))}
                      </td>
                      <td>
                        <span className="badge" style={{background: 'rgba(34, 197, 94, 0.1)', color: '#4ade80'}}>Active Sync</span>
                      </td>
                      <td style={{color: '#94a3b8', fontSize: '0.9rem'}}>{intg.last_synced_at ? new Date(intg.last_synced_at).toLocaleString() : 'Never'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      ) : (
        <div className="glass-panel" style={{maxWidth: '500px', margin: '0 auto', textAlign: 'center', padding: '3rem 2rem'}}>
          <h2 style={{marginTop: 0}}>Secure Site Check-In</h2>
          <p style={{color: '#94a3b8', marginBottom: '2rem'}}>Verify your GPS location within 500m of the site property.</p>
          
          <div className="form-group" style={{textAlign: 'left'}}>
            <label>Salesperson Name</label>
            <input className="form-input" placeholder="e.g. Rahul Sharma" value={fieldOpsData.agent_name} onChange={e => setFieldOpsData({...fieldOpsData, agent_name: e.target.value})} />
          </div>
          
          <div className="form-group" style={{textAlign: 'left'}}>
            <label>Property Site</label>
            <select className="form-input" value={fieldOpsData.site_id} onChange={e => setFieldOpsData({...fieldOpsData, site_id: e.target.value})}>
              <option value="">-- Select Property --</option>
              {sites.map(site => (
                <option key={site.id} value={site.id}>{site.name}</option>
              ))}
            </select>
          </div>

          <button className="btn-punch" onClick={handlePunchIn} disabled={punching}>
            {punching ? 'Locating GPS 📡...' : '📍 Verify GPS & Punch In'}
          </button>

          {punchStatus && (
            <div className={`punch-result ${punchStatus.punch_status === 'Valid' ? 'valid' : 'invalid'}`}>
              <h3 style={{margin: '0 0 8px 0'}}>{punchStatus.punch_status === 'Valid' ? '✅ Punch Confirmed' : '❌ Out of Bounds'}</h3>
              <p style={{margin: 0}}>You are <strong>{punchStatus.distance_m} meters</strong> away from {punchStatus.site_name}.</p>
            </div>
          )}
        </div>
      )}

      {isModalOpen && (
        <div className="modal-overlay" onClick={() => setIsModalOpen(false)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()}>
            <h2 style={{marginTop: 0, marginBottom: '2rem'}}>New Lead</h2>
            <form onSubmit={handleCreateLead}>
              <div className="form-group">
                <label>First Name</label>
                <input className="form-input" required value={formData.first_name} onChange={e => setFormData({...formData, first_name: e.target.value})} placeholder="e.g. John" />
              </div>
              <div className="form-group">
                <label>Last Name <span style={{color: '#64748b', fontSize: '0.8rem'}}>(Optional)</span></label>
                <input className="form-input" value={formData.last_name} onChange={e => setFormData({...formData, last_name: e.target.value})} placeholder="e.g. Doe" />
              </div>
              <div className="form-group">
                <label>Phone Number</label>
                <input className="form-input" required type="tel" value={formData.phone} onChange={e => setFormData({...formData, phone: e.target.value})} placeholder="+917406317771" />
              </div>
              <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '2.5rem'}}>
                <button type="button" className="btn-call" style={{borderColor: 'transparent', color: '#cbd5e1', background: 'transparent'}} onClick={() => setIsModalOpen(false)}>Cancel</button>
                <button type="submit" className="btn-primary" disabled={loading}>
                  {loading ? 'Saving...' : 'Save Lead'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {activeLeadDocs && (
        <div className="modal-overlay" onClick={() => setActiveLeadDocs(null)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()} style={{maxWidth: '600px'}}>
            <h2 style={{marginTop: 0, marginBottom: '0.5rem'}}>📁 Document Vault</h2>
            <p style={{color: '#94a3b8', marginBottom: '2rem'}}>Client: {activeLeadDocs.first_name} {activeLeadDocs.last_name}</p>
            
            <form onSubmit={handleUploadDoc} style={{display: 'flex', gap: '10px', marginBottom: '2rem', alignItems: 'flex-end'}}>
              <div className="form-group" style={{marginBottom: 0, flexGrow: 1}}>
                <label>Document Name</label>
                <input className="form-input" required value={docFormData.file_name} onChange={e => setDocFormData({...docFormData, file_name: e.target.value})} placeholder="e.g., Aadhar_Card.pdf" />
              </div>
              <div className="form-group" style={{marginBottom: 0, flexGrow: 1}}>
                <label>Mock File URL</label>
                <input className="form-input" required value={docFormData.file_url} onChange={e => setDocFormData({...docFormData, file_url: e.target.value})} placeholder="https://bdrpl.com/vault/..." />
              </div>
              <button type="submit" className="btn-primary" style={{height: '46px', padding: '0 16px'}}>Upload</button>
            </form>

            <h3 style={{fontSize: '1.1rem', marginBottom: '1rem'}}>Secure Uploads</h3>
            <div style={{maxHeight: '300px', overflowY: 'auto'}}>
              {docs.length === 0 ? (
                <div style={{padding: '2rem', textAlign: 'center', color: '#64748b', background: 'rgba(0,0,0,0.2)', borderRadius: '8px'}}>No documents found for this client.</div>
              ) : (
                <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                  {docs.map(doc => (
                    <div key={doc.id} style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: 'rgba(255,255,255,0.05)', padding: '12px 16px', borderRadius: '8px'}}>
                      <div>
                        <div style={{fontWeight: 600, color: '#e2e8f0'}}>{doc.file_name}</div>
                        <div style={{fontSize: '0.8rem', color: '#94a3b8'}}>{new Date(doc.uploaded_at).toLocaleString()}</div>
                      </div>
                      <a href={doc.file_url} target="_blank" rel="noreferrer" style={{color: '#38bdf8', textDecoration: 'none', fontSize: '0.9rem', fontWeight: 600}}>View &rarr;</a>
                    </div>
                  ))}
                </div>
              )}
            </div>

            <div style={{marginTop: '2rem', textAlign: 'right'}}>
              <button className="btn-call" style={{borderColor: 'transparent', color: '#cbd5e1', background: 'transparent'}} onClick={() => setActiveLeadDocs(null)}>Close Vault</button>
            </div>
          </div>
        </div>
      )}

      {/* AI Email Draft Modal */}
      {emailDraft && (
        <div className="modal-overlay">
          <div className="modal-content glass-panel" style={{background: 'rgba(15, 23, 42, 0.95)', border: '1px solid rgba(245, 158, 11, 0.2)'}}>
            <h2 style={{marginTop: 0, color: '#f59e0b', display: 'flex', alignItems: 'center', gap: '8px'}}>✨ GenAI Drafted Email</h2>
            
            <div style={{background: 'rgba(0,0,0,0.3)', padding: '15px', borderRadius: '8px', marginBottom: '15px', border: '1px solid rgba(255,255,255,0.05)'}}>
              <div style={{marginBottom: '10px', fontWeight: 'bold'}}>Subject: <span style={{fontWeight: 'normal', color: '#e2e8f0'}}>{emailDraft.subject}</span></div>
              <div style={{whiteSpace: 'pre-wrap', color: '#94a3b8', lineHeight: '1.5'}}>{emailDraft.body}</div>
            </div>

            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button className="btn-secondary" onClick={() => setEmailDraft(null)}>Close</button>
              <button className="btn-primary" style={{background: 'linear-gradient(135deg, #f59e0b, #dc2626)'}} onClick={() => {
                navigator.clipboard.writeText(`Subject: ${emailDraft.subject}\n\n${emailDraft.body}`);
                alert("Copied directly to clipboard!");
              }}>
                📋 Copy to Clipboard
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
