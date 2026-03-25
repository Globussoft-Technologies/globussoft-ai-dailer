import React, { useState, useEffect, useRef } from 'react';
import CallMonitor from './CallMonitor';
import KnowledgeBase from './KnowledgeBase';
import Sandbox from './Sandbox';
import './index.css';

const API_URL = "/api";

export default function App() {
  // Auth State
  const [authToken, setAuthToken] = useState(localStorage.getItem('authToken') || null);
  const apiFetch = async (url, options = {}) => {
    return fetch(url, {
      ...options,
      headers: {
        ...options.headers,
        'Authorization': `Bearer ${authToken}`
      }
    });
  };
  const [currentUser, setCurrentUser] = useState(null);
  const [authPage, setAuthPage] = useState('login'); // 'login' or 'signup'
  const [authError, setAuthError] = useState('');
  const [authLoading, setAuthLoading] = useState(false);
  const [authForm, setAuthForm] = useState({ org_name: '', full_name: '', email: 'sumit@globussoft.com', password: 'sumit1234' });

  // Check token on mount
  useEffect(() => {
    if (authToken) {
      fetch(`${API_URL}/auth/me`, { headers: { 'Authorization': `Bearer ${authToken}` } })
        .then(r => r.ok ? r.json() : Promise.reject())
        .then(u => setCurrentUser(u))
        .catch(() => { setAuthToken(null); localStorage.removeItem('authToken'); });
    }
  }, [authToken]);

  const handleLogin = async (e) => {
    e.preventDefault();
    setAuthError(''); setAuthLoading(true);
    try {
      const res = await fetch(`${API_URL}/auth/login`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: authForm.email, password: authForm.password })
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.detail || 'Login failed');
      localStorage.setItem('authToken', data.access_token);
      setAuthToken(data.access_token);
      setCurrentUser(data.user);
    } catch (err) { setAuthError(err.message); }
    setAuthLoading(false);
  };

  const handleSignup = async (e) => {
    e.preventDefault();
    setAuthError(''); setAuthLoading(true);
    try {
      const res = await fetch(`${API_URL}/auth/signup`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(authForm)
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.detail || 'Signup failed');
      localStorage.setItem('authToken', data.access_token);
      setAuthToken(data.access_token);
      setCurrentUser(data.user);
    } catch (err) { setAuthError(err.message); }
    setAuthLoading(false);
  };

  const handleLogout = () => {
    localStorage.removeItem('authToken');
    setAuthToken(null);
    setCurrentUser(null);
    setAuthForm({ org_name: '', full_name: '', email: '', password: '' });
  };


  const [activeTab, setActiveTab] = useState('crm');
  const [leads, setLeads] = useState([]);
  const [sites, setSites] = useState([]);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [dialingId, setDialingId] = useState(null);
  const [webCallActive, setWebCallActive] = useState(null);
  const webCallWsRef = useRef(null);
  const webCallAudioCtxRef = useRef(null);
  
  const [formData, setFormData] = useState({ first_name: '', last_name: '', phone: '', source: 'Manual Entry' });

  // Edit Lead State
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [editingLead, setEditingLead] = useState(null);
  const [editFormData, setEditFormData] = useState({ first_name: '', last_name: '', phone: '', source: '' });

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
  const userRole = currentUser?.role || 'Agent';

  // GenAI Email Modal State
  const [emailDraft, setEmailDraft] = useState(null);

  // Pronunciation Guide State
  const [pronunciations, setPronunciations] = useState([]);
  const [pronFormData, setPronFormData] = useState({ word: '', phonetic: '' });

  // Call Transcript State
  const [transcriptLead, setTranscriptLead] = useState(null);
  const [transcripts, setTranscripts] = useState([]);

  // Product Knowledge State
  const [orgs, setOrgs] = useState([]);
  const [selectedOrg, setSelectedOrg] = useState(null);
  const [orgProducts, setOrgProducts] = useState([]);
  const [scraping, setScraping] = useState(null); // product_id being scraped
  const [newOrgName, setNewOrgName] = useState('');
  const [showOrgInput, setShowOrgInput] = useState(false);
  const [newProductName, setNewProductName] = useState('');
  const [showProductInput, setShowProductInput] = useState(false);
  const [systemPromptAuto, setSystemPromptAuto] = useState('');
  const [systemPromptCustom, setSystemPromptCustom] = useState('');
  const [promptSaving, setPromptSaving] = useState(false);
  const [promptDirty, setPromptDirty] = useState(false);
  const [activeVoiceProvider, setActiveVoiceProvider] = useState('elevenlabs');
  const [activeVoiceId, setActiveVoiceId] = useState('');
  const [savedVoiceName, setSavedVoiceName] = useState('');
  const [activeLanguage, setActiveLanguage] = useState('hi');

  const INDIAN_LANGUAGES = [
    { code: 'hi', name: 'Hindi' },
    { code: 'ta', name: 'Tamil' },
    { code: 'te', name: 'Telugu' },
    { code: 'kn', name: 'Kannada' },
    { code: 'ml', name: 'Malayalam' },
    { code: 'mr', name: 'Marathi' },
    { code: 'gu', name: 'Gujarati' },
    { code: 'bn', name: 'Bengali' },
    { code: 'pa', name: 'Punjabi' },
    { code: 'en', name: 'English' },
  ];

  const INDIAN_VOICES = {
    elevenlabs: [
      { id: 'oH8YmZXJYEZq5ScgoGn9', name: 'Aakash – Friendly Support' },
      { id: 'X4ExprIXDKrWcHdtGysh', name: 'Anjura – Confident' },
      { id: 'SXuKWBhKoIoAHKlf6Gt3', name: 'Gaurav – Professional' },
      { id: 'N09NFwYJJG9VSSgdLQbT', name: 'Ishan – Bold & Upbeat' },
      { id: 'U9wNM2BNANqtBCawWLgA', name: 'Himanshu – Calm' },
      { id: 'h061KGyOtpLYDxcoi8E3', name: 'Ravi – Gentle' },
      { id: 'Ock0AL5DBkvTUDePt4Hm', name: 'Viraj – Commanding' },
      { id: 'nwj0s2LU9bDWRKND5yzA', name: 'Bunty – Fun' },
      { id: 'amiAXapsDOAiHJqbsAZj', name: 'Priya – Confident ♀' },
      { id: '6JsmTroalVewG1gA6Jmw', name: 'Sia – Friendly ♀' },
      { id: '9vP6R7VVxNwGIGLnpl17', name: 'Suhana – Joyful ♀' },
      { id: 'hO2yZ8lxM3axUxL8OeKX', name: 'Mini – Cute ♀' },
      { id: 's0oIsoSJ9raiUm7DJNzW', name: '⭐ Default Voice' },
    ],
    smallest: [
      { id: 'mithali', name: 'Mithali ♀' },
      { id: 'priya', name: 'Priya ♀' },
      { id: 'aravind', name: 'Aravind ♂' },
      { id: 'raj', name: 'Raj ♂' },
      { id: 'arman', name: 'Arman ♂' },
      { id: 'jasmine', name: 'Jasmine ♀' },
    ]
  };

  // Auth block moved down to fix React hooks violation

  const fetchLeads = async () => {
    try {
      const res = await apiFetch(`${API_URL}/leads`);
      const data = await res.json();
      setLeads(data);
    } catch (e) {
      console.error("Make sure FastAPI is running with CORS enabled!", e);
    }
  };

  const fetchSites = async () => {
    try {
      const res = await apiFetch(`${API_URL}/sites`);
      setSites(await res.json());
    } catch (e) {
      console.error("Could not fetch sites:", e);
    }
  };

  const fetchTasks = async () => {
    try { const res = await apiFetch(`${API_URL}/tasks`); setTasks(await res.json()); } catch(e){}
  };

  const fetchReports = async () => {
    try { const res = await apiFetch(`${API_URL}/reports`); setReports(await res.json()); } catch(e){}
  };

  const fetchWhatsappLogs = async () => {
    try { const res = await apiFetch(`${API_URL}/whatsapp`); setWhatsappLogs(await res.json()); } catch(e){}
  };

  const fetchAnalytics = async () => {
    try { const res = await apiFetch(`${API_URL}/analytics`); setAnalyticsData(await res.json()); } catch(e){}
  };

  const fetchIntegrations = async () => {
    try { const res = await apiFetch(`${API_URL}/integrations`); setIntegrations(await res.json()); } catch(e){}
  };

  const fetchPronunciations = async () => {
    try { const res = await apiFetch(`${API_URL}/pronunciation`); setPronunciations(await res.json()); } catch(e){}
  };

  const fetchOrgs = async () => {
    try {
      const res = await apiFetch(`${API_URL}/organizations`);
      const data = await res.json();
      setOrgs(data);
      // Auto-select user's org if only one
      if (data.length === 1 && !selectedOrg) {
        setSelectedOrg(data[0]);
        fetchOrgProducts(data[0].id);
        fetchSystemPrompt(data[0].id);
        // Load voice settings
        try {
          const vRes = await apiFetch(`${API_URL}/organizations/${data[0].id}/voice-settings`);
          const vs = await vRes.json();
          if (vs.tts_provider) {
            setActiveVoiceProvider(vs.tts_provider);
            if (vs.tts_voice_id) {
              setActiveVoiceId(vs.tts_voice_id);
              const allV = [...(INDIAN_VOICES[vs.tts_provider] || []), ...(INDIAN_VOICES.elevenlabs || []), ...(INDIAN_VOICES.smallest || [])];
              const found = allV.find(v => v.id === vs.tts_voice_id);
              if (found) setSavedVoiceName(found.name);
            }
            if (vs.tts_language) setActiveLanguage(vs.tts_language);
          }
        } catch(e){}
      }
    } catch(e){}
  };

  const fetchOrgProducts = async (orgId) => {
    try { const res = await apiFetch(`${API_URL}/organizations/${orgId}/products`); setOrgProducts(await res.json()); } catch(e){}
  };

  useEffect(() => {
    if (!currentUser) return;
    fetchLeads();
    fetchSites();
    fetchTasks();
    fetchReports();
    fetchWhatsappLogs();
    fetchAnalytics();
    fetchPronunciations();
    fetchOrgs();
  }, [currentUser]);

  const handleStatusChange = async (leadId, newStatus) => {
    try {
      await apiFetch(`${API_URL}/leads/${leadId}/status`, {
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
      await apiFetch(`${API_URL}/tasks/${taskId}/complete`, { method: 'PUT' });
      fetchTasks();
      fetchReports();
    } catch (e) { console.error(e); }
  };

  const handleCreateLead = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/leads`, {
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
      const res = await apiFetch(`${API_URL}/dial/${lead.id}`, { method: "POST" });
      const data = await res.json();
      alert(`Status: ${data.message || 'Connecting call...'}`);
    } catch(e) {
      alert("Failed to hit the dialer API. Check console.");
    }
    setTimeout(() => setDialingId(null), 3000);
  };

  const handleWebCall = async (lead) => {
    if (webCallActive === lead.id) {
      // Disconnect active simulation
      if (webCallWsRef.current) webCallWsRef.current.close();
      if (webCallAudioCtxRef.current) webCallAudioCtxRef.current.close();
      setWebCallActive(null);
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const audioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 8000 });
      webCallAudioCtxRef.current = audioContext;

      // Create a destination node to capture mixed audio for recording
      const recDest = audioContext.createMediaStreamDestination();
      const mediaRecorder = new MediaRecorder(recDest.stream, { mimeType: 'audio/webm;codecs=opus' });
      const recordedChunks = [];
      mediaRecorder.ondataavailable = (e) => { if (e.data.size > 0) recordedChunks.push(e.data); };
      mediaRecorder.start(1000); // collect chunks every 1s

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.hostname;
      
      const qp = new URLSearchParams({
        name: lead.first_name || 'Customer',
        phone: lead.phone || '',
        interest: lead.interest || (orgProducts.length > 0 ? orgProducts[0].name : 'our platform'),
        lead_id: String(lead.id || ''),
        tts_provider: activeVoiceProvider,
        voice: activeVoiceId,
        tts_language: activeLanguage,
      }).toString();

      let wsUrl;
      if (host === 'localhost' || host === '127.0.0.1') {
        wsUrl = `ws://${host}:8001/media-stream?${qp}`;
      } else {
        wsUrl = `${protocol}//${window.location.host}/media-stream?${qp}`;
      }
      
      const ws = new WebSocket(wsUrl);
      webCallWsRef.current = ws;

      ws.onopen = () => {
        setWebCallActive(lead.id);
        ws.send(JSON.stringify({ event: 'connected' }));
        const sid = `web_sim_${lead.id}_${Date.now()}`;
        ws.send(JSON.stringify({ event: 'start', start: { stream_sid: sid }, stream_sid: sid }));

        const source = audioContext.createMediaStreamSource(stream);
        const processor = audioContext.createScriptProcessor(2048, 1, 1);

        source.connect(processor);
        processor.connect(audioContext.destination);
        // Also route mic to recording destination
        source.connect(recDest);

        // Echo suppression: mute mic while AI speaks through speakers
        let micMuted = true; // Start muted until greeting finishes
        let unmuteTimer = null;

        processor.onaudioprocess = (e) => {
          if (ws.readyState !== WebSocket.OPEN) return;
          if (micMuted) return; // Don't send mic audio while AI is speaking
          const float32Array = e.inputBuffer.getChannelData(0);
          
          const int16Buffer = new Int16Array(float32Array.length);
          for (let i = 0; i < float32Array.length; i++) {
            let s = Math.max(-1, Math.min(1, float32Array[i]));
            int16Buffer[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
          }
          
          let binary = '';
          const bytes = new Uint8Array(int16Buffer.buffer);
          for (let i = 0; i < bytes.byteLength; i++) {
            binary += String.fromCharCode(bytes[i]);
          }
          const base64 = window.btoa(binary);

          ws.send(JSON.stringify({
            event: 'media',
            media: { payload: base64 }
          }));
        };

        let nextPlayTime = audioContext.currentTime;
        ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          if (data.event === 'media') {
            // Mute mic while AI is talking to prevent echo feedback
            micMuted = true;
            if (unmuteTimer) clearTimeout(unmuteTimer);

            const audioStr = window.atob(data.media.payload);
            const audioBytes = new Uint8Array(audioStr.length);
            for (let i = 0; i < audioStr.length; i++) {
              audioBytes[i] = audioStr.charCodeAt(i);
            }
            const int16Array = new Int16Array(audioBytes.buffer);
            const float32Array = new Float32Array(int16Array.length);
            for (let i = 0; i < int16Array.length; i++) {
              float32Array[i] = int16Array[i] / 0x8000;
            }
            
            const buffer = audioContext.createBuffer(1, float32Array.length, 8000);
            buffer.getChannelData(0).set(float32Array);
            
            const destSource = audioContext.createBufferSource();
            destSource.buffer = buffer;
            destSource.connect(audioContext.destination);
            // Also route TTS to recording destination
            destSource.connect(recDest);
            
            if (audioContext.currentTime > nextPlayTime) nextPlayTime = audioContext.currentTime;
            destSource.start(nextPlayTime);
            nextPlayTime += buffer.duration;

            // Unmute mic 500ms after last TTS chunk finishes playing
            const remainingPlayMs = Math.max(0, (nextPlayTime - audioContext.currentTime) * 1000) + 500;
            unmuteTimer = setTimeout(() => { micMuted = false; }, remainingPlayMs);
          } else if (data.event === 'clear') {
            nextPlayTime = audioContext.currentTime; // Discard TTS queue on barge-in
            micMuted = false; // Immediately unmute on barge-in clear
            if (unmuteTimer) clearTimeout(unmuteTimer);
          }
        };

        ws.onclose = () => {
          stream.getTracks().forEach(track => track.stop());
          // Stop recording and upload
          if (mediaRecorder.state !== 'inactive') {
            mediaRecorder.stop();
            mediaRecorder.onstop = async () => {
              if (recordedChunks.length > 0) {
                const blob = new Blob(recordedChunks, { type: 'audio/webm' });
                const formData = new FormData();
                formData.append('file', blob, `call_${lead.id}_${Date.now()}.webm`);
                formData.append('lead_id', String(lead.id));
                try {
                  await apiFetch(`${API_URL}/upload-recording`, { method: 'POST', body: formData });
                } catch(e) { console.error('Recording upload failed:', e); }
              }
            };
          }
          if (webCallAudioCtxRef.current) webCallAudioCtxRef.current.close();
          setWebCallActive(null);
        };
      };
    } catch (e) {
      alert("Microphone access denied or connection to WebSockets failed.");
      console.error(e);
      setWebCallActive(null);
    }
  };

  const handleOpenDocs = async (lead) => {
    setActiveLeadDocs(lead);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/documents`);
      setDocs(await res.json());
    } catch(e) {}
  };

  const handleUploadDoc = async (e) => {
    e.preventDefault();
    try {
      await apiFetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(docFormData)
      });
      setDocFormData({ file_name: '', file_url: '' });
      const res = await apiFetch(`${API_URL}/leads/${activeLeadDocs.id}/documents`);
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
        const response = await apiFetch(`${API_URL}/punch`, {
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
        const res = await apiFetch(`${API_URL}/leads/search?q=${encodeURIComponent(query)}`);
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
        await apiFetch(`${API_URL}/leads/${lead.id}/notes`, {
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
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/draft-email`);
      const data = await res.json();
      setEmailDraft(data);
    } catch(e) {
      console.error("Error generating email", e);
    }
    setDialingId(null);
  };

  const handleEditLead = (lead) => {
    setEditingLead(lead);
    setEditFormData({
      first_name: lead.first_name || '',
      last_name: lead.last_name || '',
      phone: lead.phone || '',
      source: lead.source || 'Manual Entry'
    });
    setEditModalOpen(true);
  };

  const handleSaveEdit = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/leads/${editingLead.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editFormData)
      });
      setEditModalOpen(false);
      setEditingLead(null);
      fetchLeads();
    } catch (e) {
      console.error('Error updating lead', e);
    }
    setLoading(false);
  };

  const handleDeleteLead = async (lead) => {
    if (!window.confirm(`Are you sure you want to delete ${lead.first_name} ${lead.last_name}?`)) return;
    try {
      await apiFetch(`${API_URL}/leads/${lead.id}`, { method: 'DELETE' });
      fetchLeads();
    } catch (e) {
      console.error('Error deleting lead', e);
    }
  };

  const handleCreateIntegration = async (e) => {
    e.preventDefault();
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/integrations`, {
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

  const handleAddPronunciation = async (e) => {
    e.preventDefault();
    if (!pronFormData.word.trim() || !pronFormData.phonetic.trim()) return;
    try {
      await apiFetch(`${API_URL}/pronunciation`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(pronFormData)
      });
      setPronFormData({ word: '', phonetic: '' });
      fetchPronunciations();
    } catch(e) { console.error(e); }
  };

  const handleDeletePronunciation = async (id) => {
    try {
      await apiFetch(`${API_URL}/pronunciation/${id}`, { method: 'DELETE' });
      fetchPronunciations();
    } catch(e) { console.error(e); }
  };

  const handleViewTranscripts = async (lead) => {
    setTranscriptLead(lead);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/transcripts`);
      setTranscripts(await res.json());
    } catch(e) { setTranscripts([]); }
  };

  const handleCreateOrg = async () => {
    if (!newOrgName.trim()) return;
    await apiFetch(`${API_URL}/organizations`, { method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({ name: newOrgName.trim() }) });
    setNewOrgName(''); setShowOrgInput(false);
    fetchOrgs();
  };

  const handleDeleteOrg = async (orgId) => {
    if (!confirm('Delete this organization and all its products?')) return;
    await apiFetch(`${API_URL}/organizations/${orgId}`, { method: 'DELETE' });
    if (selectedOrg?.id === orgId) { setSelectedOrg(null); setOrgProducts([]); }
    fetchOrgs();
  };

  const handleSelectOrg = (org) => {
    setSelectedOrg(org);
    setShowProductInput(false); setNewProductName('');
    fetchOrgProducts(org.id);
    fetchSystemPrompt(org.id);
  };

  const handleAddProduct = async () => {
    if (!selectedOrg || !newProductName.trim()) return;
    await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/products`, {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ name: newProductName.trim() })
    });
    setNewProductName(''); setShowProductInput(false);
    fetchOrgProducts(selectedOrg.id);
  };

  const handleScrapeProduct = async (productId) => {
    setScraping(productId);
    try {
      const res = await apiFetch(`${API_URL}/products/${productId}/scrape`, { method: 'POST' });
      const data = await res.json();
      if (data.scraped_info) fetchOrgProducts(selectedOrg.id);
    } catch(e) { console.error(e); }
    setScraping(null);
  };

  const handleSaveProduct = async (productId, updates) => {
    await apiFetch(`${API_URL}/products/${productId}`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(updates)
    });
    fetchOrgProducts(selectedOrg.id);
    // Refresh system prompt preview after product update
    if (selectedOrg) fetchSystemPrompt(selectedOrg.id);
  };

  const fetchSystemPrompt = async (orgId) => {
    try {
      const res = await apiFetch(`${API_URL}/organizations/${orgId}/system-prompt`);
      const data = await res.json();
      setSystemPromptAuto(data.auto_generated || '');
      setSystemPromptCustom(data.custom_prompt || '');
      setPromptDirty(false);
    } catch(e) {}
  };

  const handleSaveSystemPrompt = async () => {
    if (!selectedOrg) return;
    setPromptSaving(true);
    await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/system-prompt`, {
      method: 'PUT', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ custom_prompt: systemPromptCustom })
    });
    setPromptSaving(false);
    setPromptDirty(false);
  };

  const handleDeleteProduct = async (productId) => {
    if (!confirm('Delete this product?')) return;
    await apiFetch(`${API_URL}/products/${productId}`, { method: 'DELETE' });
    fetchOrgProducts(selectedOrg.id);
  };

  // ─── AUTH PAGES (after all hooks) ───
  if (!authToken || !currentUser) {
    return (
      <div style={{minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center',
        background: 'linear-gradient(135deg, #0f0c29, #302b63, #24243e)', padding: '20px'}}>
        <div style={{width: '100%', maxWidth: '440px'}}>
          <div style={{textAlign: 'center', marginBottom: '2rem'}}>
            <h1 style={{fontSize: '2rem', fontWeight: 800, background: 'linear-gradient(135deg, #a78bfa, #22d3ee)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent'}}>
              🤖 Callified AI
            </h1>
            <p style={{color: '#94a3b8', fontSize: '0.95rem'}}>AI-Powered Lead Qualification Platform</p>
          </div>

          <div className="glass-panel" style={{padding: '2rem'}}>
            <div style={{display: 'flex', marginBottom: '1.5rem', borderRadius: '8px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.1)'}}>
              <button onClick={() => { setAuthPage('login'); setAuthError(''); }}
                style={{flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                  background: authPage === 'login' ? 'rgba(167,139,250,0.2)' : 'transparent',
                  color: authPage === 'login' ? '#a78bfa' : '#64748b'}}>
                Login
              </button>
              <button onClick={() => { setAuthPage('signup'); setAuthError(''); }}
                style={{flex: 1, padding: '10px', border: 'none', cursor: 'pointer', fontWeight: 600, fontSize: '0.9rem',
                  background: authPage === 'signup' ? 'rgba(34,211,238,0.2)' : 'transparent',
                  color: authPage === 'signup' ? '#22d3ee' : '#64748b'}}>
                Sign Up
              </button>
            </div>

            {authError && (
              <div style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '8px', padding: '10px 14px', marginBottom: '1rem', color: '#fca5a5', fontSize: '0.85rem'}}>
                {authError}
              </div>
            )}

            <form onSubmit={authPage === 'login' ? handleLogin : handleSignup}>
              {authPage === 'signup' && (
                <>
                  <div className="form-group">
                    <label>Organization Name</label>
                    <input className="form-input" placeholder="e.g. Globussoft" required
                      value={authForm.org_name} onChange={e => setAuthForm({...authForm, org_name: e.target.value})} />
                  </div>
                  <div className="form-group">
                    <label>Your Full Name</label>
                    <input className="form-input" placeholder="e.g. Sumit Kumar" required
                      value={authForm.full_name} onChange={e => setAuthForm({...authForm, full_name: e.target.value})} />
                  </div>
                </>
              )}
              <div className="form-group">
                <label>Email</label>
                <input className="form-input" type="email" placeholder="you@company.com" required
                  value={authForm.email} onChange={e => setAuthForm({...authForm, email: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Password</label>
                <input className="form-input" type="password" placeholder="••••••••" required minLength={6}
                  value={authForm.password} onChange={e => setAuthForm({...authForm, password: e.target.value})} />
              </div>
              <button type="submit" className="btn-primary" disabled={authLoading}
                style={{width: '100%', padding: '12px', marginTop: '0.5rem', fontSize: '1rem', fontWeight: 700,
                  background: authPage === 'login' ? 'linear-gradient(135deg, #a78bfa, #7c3aed)' : 'linear-gradient(135deg, #22d3ee, #06b6d4)'}}>
                {authLoading ? '⏳ Please wait...' : (authPage === 'login' ? '🔐 Login' : '🚀 Create Account')}
              </button>
            </form>
          </div>
        </div>
      </div>
    );
  }

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
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'integrations' ? 'active' : ''}`} onClick={() => setActiveTab('integrations')}>🔌 Integrations</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'monitor' ? 'active' : ''}`} onClick={() => setActiveTab('monitor')}>🎙️ Monitor AI Calls</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'knowledge' ? 'active' : ''}`} onClick={() => setActiveTab('knowledge')}>🧠 RAG Knowledge</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'sandbox' ? 'active' : ''}`} onClick={() => setActiveTab('sandbox')}>🎯 AI Sandbox</button>}
          {userRole === 'Admin' && <button className={`tab-btn ${activeTab === 'settings' ? 'active' : ''}`} onClick={() => setActiveTab('settings')}>⚙️ Settings</button>}
          
          <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '12px'}}>
            {currentUser && (
              <span style={{fontSize: '0.8rem', color: '#94a3b8', letterSpacing: '0.5px'}}>
                👤 {currentUser.full_name || currentUser.email} {currentUser.org_name ? `(${currentUser.org_name})` : ''}
              </span>
            )}
            <button onClick={handleLogout}
              style={{background: 'rgba(239,68,68,0.15)', border: '1px solid rgba(239,68,68,0.3)', borderRadius: '6px',
                color: '#fca5a5', padding: '6px 14px', cursor: 'pointer', fontWeight: 600, fontSize: '0.8rem'}}>
              🚪 Logout
            </button>
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
              {userRole === 'Admin' && (<>
                <button className="btn-call" style={{borderColor: '#22c55e', color: '#22c55e', padding: '0 16px', height: '40px', background: 'rgba(34, 197, 94, 0.1)', cursor: 'pointer'}} onClick={() => window.open(`${API_URL}/leads/export`, '_blank')}>
                  📥 Export CSV
                </button>
                <button className="btn-call" style={{borderColor: '#3b82f6', color: '#3b82f6', padding: '0 16px', height: '40px', background: 'rgba(59, 130, 246, 0.1)', cursor: 'pointer'}} onClick={() => document.getElementById('csv-import-input').click()}>
                  📤 Import CSV
                </button>
                <input id="csv-import-input" type="file" accept=".csv" style={{display: 'none'}} onChange={async (e) => {
                  const f = e.target.files[0]; if (!f) return;
                  const fd = new FormData(); fd.append('file', f);
                  try {
                    const r = await fetch(`${API_URL}/leads/import-csv`, {method: 'POST', headers: {'Authorization': `Bearer ${token}`}, body: fd});
                    const d = await r.json();
                    alert(`✅ Imported ${d.imported} leads` + (d.errors?.length ? `\n⚠️ Errors:\n${d.errors.join('\n')}` : ''));
                    fetchLeads();
                  } catch (err) { alert('Import failed: ' + err.message); }
                  e.target.value = '';
                }} />
                <a href={`${API_URL}/leads/sample-csv`} style={{color: '#94a3b8', fontSize: '13px', alignSelf: 'center', textDecoration: 'underline', cursor: 'pointer'}}>📋 Sample CSV</a>
              </>)}
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
            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', flexWrap: 'wrap', gap: '12px'}}>
              <h2 style={{marginTop: 0, marginBottom: 0, fontSize: '1.25rem', fontWeight: 600}}>Campaign Leads</h2>
              <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                <span style={{fontSize: '0.8rem', color: '#64748b', fontWeight: 600}}>🔊 Voice:</span>
                <select className="form-input" value={activeVoiceProvider}
                  onChange={e => { setActiveVoiceProvider(e.target.value); setActiveVoiceId(INDIAN_VOICES[e.target.value]?.[0]?.id || ''); }}
                  style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '100px'}}>
                  <option value="elevenlabs">ElevenLabs</option>
                  <option value="smallest">Smallest AI</option>
                </select>
                <select className="form-input" value={activeVoiceId}
                  onChange={e => setActiveVoiceId(e.target.value)}
                  style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '160px'}}>
                  {(INDIAN_VOICES[activeVoiceProvider] || []).map(v => (
                    <option key={v.id} value={v.id}>{v.name}</option>
                  ))}
                </select>
                <select className="form-input" value={activeLanguage}
                  onChange={e => setActiveLanguage(e.target.value)}
                  style={{width: 'auto', height: '32px', fontSize: '0.8rem', padding: '4px 8px', minWidth: '90px'}}>
                  {INDIAN_LANGUAGES.map(l => (
                    <option key={l.code} value={l.code}>{l.name}</option>
                  ))}
                </select>
                <button style={{background: 'linear-gradient(135deg, #8b5cf6, #6d28d9)', border: 'none', color: '#fff', fontSize: '0.75rem', padding: '6px 10px', borderRadius: '6px', cursor: 'pointer', whiteSpace: 'nowrap'}}
                  onClick={async () => {
                    if (!selectedOrg) return;
                    await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/voice-settings`, {
                      method: 'PUT', headers: {'Content-Type': 'application/json'},
                      body: JSON.stringify({ tts_provider: activeVoiceProvider, tts_voice_id: activeVoiceId, tts_language: activeLanguage })
                    });
                    const vName = (INDIAN_VOICES[activeVoiceProvider] || []).find(v => v.id === activeVoiceId)?.name || activeVoiceId;
                    setSavedVoiceName(vName);
                  }}>💾 Save Default</button>
              </div>
              {savedVoiceName && (
                <div style={{fontSize: '0.75rem', color: '#a78bfa', marginTop: '4px', textAlign: 'right'}}>✅ Default voice: <strong>{savedVoiceName}</strong></div>
              )}
            </div>
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
                            style={{background: 'rgba(250, 204, 21, 0.15)', color: '#facc15', borderColor: 'rgba(250, 204, 21, 0.3)', padding: '4px 10px', fontSize: '0.8rem'}}
                            onClick={() => handleEditLead(lead)}
                          >
                            ✏️ Edit
                          </button>
                          <button 
                            className="btn-call" 
                            style={{background: 'rgba(239, 68, 68, 0.15)', color: '#ef4444', borderColor: 'rgba(239, 68, 68, 0.3)', padding: '4px 10px', fontSize: '0.8rem'}}
                            onClick={() => handleDeleteLead(lead)}
                          >
                            🗑️
                          </button>
                          <button 
                            className="btn-call" 
                            style={{background: 'rgba(56, 189, 248, 0.15)', color: '#38bdf8', borderColor: 'rgba(56, 189, 248, 0.3)'}}
                            onClick={() => handleOpenDocs(lead)}
                          >
                            📁 Docs
                          </button>
                          <button 
                            className="btn-call" 
                            style={{background: 'rgba(99, 102, 241, 0.15)', color: '#818cf8', borderColor: 'rgba(99, 102, 241, 0.3)'}}
                            onClick={() => handleViewTranscripts(lead)}
                          >
                            📋 Transcript
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
                            style={{
                              background: webCallActive === lead.id ? '#ef4444' : 'linear-gradient(135deg, rgba(34, 211, 238, 0.15), rgba(14, 165, 233, 0.15))', 
                              color: webCallActive === lead.id ? '#ffffff' : '#38bdf8', 
                              borderColor: webCallActive === lead.id ? '#ef4444' : 'rgba(34, 211, 238, 0.3)',
                              boxShadow: webCallActive === lead.id ? '0 0 12px rgba(239, 68, 68, 0.6)' : 'none',
                              fontWeight: webCallActive === lead.id ? 700 : 500
                            }}
                            onClick={() => handleWebCall(lead)}
                          >
                            {webCallActive === lead.id ? '🛑 End Live Sim' : '🎙️ Sim Web Call'}
                          </button>
                          <button 
                            className="btn-call" 
                            onClick={() => handleDial(lead)}
                            disabled={dialingId === lead.id}
                          >
                            {dialingId === lead.id ? 'Dialing...' : '📞 Exotel'}
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
      ) : activeTab === 'monitor' ? (
        <div style={{padding: '1rem'}}>
          <CallMonitor apiUrl={API_URL} />
        </div>
      ) : activeTab === 'knowledge' ? (
        <div style={{padding: '1rem'}}>
          <KnowledgeBase apiUrl={API_URL} />
        </div>
      ) : activeTab === 'sandbox' ? (
        <div style={{padding: '1rem'}}>
          <Sandbox apiUrl={API_URL} />
        </div>
      ) : activeTab === 'settings' ? (
        <div style={{padding: '1rem', maxWidth: '800px', margin: '0 auto'}}>
          <div className="wa-header" style={{borderBottom: '1px solid rgba(255,255,255,0.05)', marginBottom: '2rem'}}>
            <h3><span style={{color: '#f59e0b'}}>AI Voice</span> Settings</h3>
            <p>Configure how the AI pronounces product names, brand names, and technical terms during calls.</p>
          </div>

          <div className="glass-panel" style={{marginBottom: '2rem'}}>
            <h4 style={{marginTop: 0, marginBottom: '1.5rem', fontSize: '1.1rem', fontWeight: 600}}>🗣️ Pronunciation Guide</h4>
            <p style={{color: '#94a3b8', fontSize: '0.9rem', marginBottom: '1.5rem'}}>
              Teach the AI how to speak your product names correctly. The AI will use the phonetic version in conversations.
            </p>

            <form onSubmit={handleAddPronunciation} style={{display: 'flex', gap: '12px', marginBottom: '2rem', alignItems: 'flex-end'}}>
              <div className="form-group" style={{marginBottom: 0, flex: 1}}>
                <label>Written Word</label>
                <input 
                  className="form-input" 
                  required 
                  value={pronFormData.word} 
                  onChange={e => setPronFormData({...pronFormData, word: e.target.value})} 
                  placeholder="e.g. Adsgpt" 
                />
              </div>
              <div style={{fontSize: '1.5rem', color: '#64748b', paddingBottom: '8px'}}>→</div>
              <div className="form-group" style={{marginBottom: 0, flex: 1}}>
                <label>How to Pronounce</label>
                <input 
                  className="form-input" 
                  required 
                  value={pronFormData.phonetic} 
                  onChange={e => setPronFormData({...pronFormData, phonetic: e.target.value})} 
                  placeholder="e.g. Ads G P T" 
                />
              </div>
              <button type="submit" className="btn-primary" style={{height: '46px', padding: '0 20px', whiteSpace: 'nowrap'}}>
                + Add Rule
              </button>
            </form>

            {pronunciations.length === 0 ? (
              <div style={{padding: '2rem', textAlign: 'center', color: '#64748b', background: 'rgba(0,0,0,0.2)', borderRadius: '8px'}}>
                No pronunciation rules added yet. Add one above to get started!
              </div>
            ) : (
              <table className="leads-table">
                <thead>
                  <tr>
                    <th>Written Word</th>
                    <th>AI Says</th>
                    <th>Added</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {pronunciations.map(p => (
                    <tr key={p.id}>
                      <td style={{fontWeight: 600, color: '#e2e8f0', fontFamily: 'monospace'}}>{p.word}</td>
                      <td style={{color: '#4ade80', fontStyle: 'italic'}}>🔊 "{p.phonetic}"</td>
                      <td style={{color: '#94a3b8', fontSize: '0.85rem'}}>{p.created_at ? new Date(p.created_at).toLocaleDateString() : '—'}</td>
                      <td>
                        <button 
                          className="btn-call" 
                          style={{background: 'rgba(239, 68, 68, 0.15)', color: '#ef4444', borderColor: 'rgba(239, 68, 68, 0.3)', padding: '4px 12px', fontSize: '0.8rem'}}
                          onClick={() => handleDeletePronunciation(p.id)}
                        >
                          🗑️ Remove
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          <div className="glass-panel" style={{background: 'rgba(245, 158, 11, 0.05)', border: '1px solid rgba(245, 158, 11, 0.15)'}}>
            <h4 style={{marginTop: 0, color: '#f59e0b', fontSize: '0.95rem'}}>💡 How it works</h4>
            <p style={{color: '#94a3b8', fontSize: '0.85rem', margin: 0, lineHeight: 1.7}}>
              The pronunciation guide is injected into the AI's prompt at the start of every call.
              When the AI generates a response containing a mapped word, it will use the phonetic version instead.
              The TTS engine then speaks the phonetic text, resulting in correct pronunciation.
              <br/><br/>
              <strong style={{color: '#e2e8f0'}}>Example:</strong> If you add "Adsgpt" → "Ads G P T", the AI will say "Ads G P T" instead of trying to sound out "Adsgpt".
            </p>
          </div>

          {/* Product Knowledge Section */}
          <div className="wa-header" style={{borderBottom: '1px solid rgba(255,255,255,0.05)', margin: '2.5rem 0 1.5rem'}}>
            <h3><span style={{color: '#22d3ee'}}>🌐 Product</span> Knowledge</h3>
            <p>Manage your organizations and products. The AI learns from this to have informed conversations.</p>
          </div>

          <div className="glass-panel" style={{marginBottom: '2rem', display: 'flex', alignItems: 'center', gap: '12px', padding: '1rem 1.5rem'}}>
            <span style={{fontSize: '1.3rem'}}>🏛️</span>
            <div>
              <div style={{fontSize: '0.75rem', color: '#64748b', fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.05em'}}>Your Organization</div>
              <div style={{fontSize: '1.15rem', fontWeight: 700, color: '#22d3ee'}}>{selectedOrg ? selectedOrg.name : (orgs.length > 0 ? orgs[0].name : 'No organization linked')}</div>
            </div>
          </div>

          {selectedOrg && (
            <div className="glass-panel" style={{marginBottom: '2rem'}}>
              <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                <h4 style={{marginTop: 0, marginBottom: 0, fontSize: '1.1rem', fontWeight: 600, color: '#22d3ee'}}>📦 Products in {selectedOrg.name}</h4>
                {!showProductInput ? (
                  <button className="btn-primary" style={{background: 'linear-gradient(135deg, #22d3ee, #06b6d4)', fontSize: '0.85rem', padding: '6px 14px'}}
                    onClick={() => setShowProductInput(true)}>+ Add Product</button>
                ) : (
                  <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                    <input className="form-input" autoFocus placeholder="Product name (e.g. AdsGPT)..."
                      value={newProductName} onChange={e => setNewProductName(e.target.value)}
                      onKeyDown={e => e.key === 'Enter' && handleAddProduct()}
                      style={{width: '220px', height: '36px', fontSize: '0.85rem'}} />
                    <button className="btn-primary" style={{background: 'linear-gradient(135deg, #10b981, #059669)', fontSize: '0.85rem', padding: '6px 14px', height: '36px'}}
                      onClick={handleAddProduct}>Add</button>
                    <button style={{background: 'transparent', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', fontSize: '0.85rem', padding: '6px 10px', borderRadius: '6px', cursor: 'pointer', height: '36px'}}
                      onClick={() => { setShowProductInput(false); setNewProductName(''); }}>✕</button>
                  </div>
                )}
              </div>

              {orgProducts.length === 0 ? (
                <div style={{padding: '1.5rem', textAlign: 'center', color: '#64748b', background: 'rgba(0,0,0,0.2)', borderRadius: '8px'}}>No products yet. Add one to configure AI knowledge.</div>
              ) : (
                <div style={{display: 'flex', flexDirection: 'column', gap: '16px'}}>
                  {orgProducts.map(p => (
                    <div key={p.id} style={{background: 'rgba(0,0,0,0.2)', borderRadius: '12px', padding: '1.25rem', border: '1px solid rgba(255,255,255,0.05)'}}>
                      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                        <span style={{fontWeight: 700, fontSize: '1.05rem', color: '#e2e8f0'}}>{p.name}</span>
                        <button style={{background: 'transparent', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: '0.85rem'}}
                          onClick={() => handleDeleteProduct(p.id)}>🗑️ Remove</button>
                      </div>

                      <div style={{display: 'flex', gap: '10px', marginBottom: '1rem', alignItems: 'flex-end'}}>
                        <div className="form-group" style={{marginBottom: 0, flex: 1}}>
                          <label>Website URL</label>
                          <input className="form-input" placeholder="https://..." defaultValue={p.website_url}
                            onBlur={e => handleSaveProduct(p.id, { website_url: e.target.value })} />
                        </div>
                        <button className="btn-primary" style={{height: '42px', padding: '0 16px', whiteSpace: 'nowrap',
                          background: scraping === p.id ? '#475569' : 'linear-gradient(135deg, #06b6d4, #0891b2)', fontSize: '0.85rem'}}
                          onClick={() => handleScrapeProduct(p.id)} disabled={scraping === p.id}>
                          {scraping === p.id ? '⏳ Analyzing...' : (p.website_url ? '🔍 Scrape Website' : '🧠 AI Research')}
                        </button>
                      </div>

                      {p.scraped_info && (
                        <div style={{marginBottom: '1rem'}}>
                          <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, color: '#22d3ee', fontSize: '0.85rem'}}>📄 AI-Extracted Info</label>
                          <div style={{background: 'rgba(0,0,0,0.3)', padding: '12px', borderRadius: '8px',
                            border: '1px solid rgba(34, 211, 238, 0.15)', whiteSpace: 'pre-wrap',
                            color: '#cbd5e1', fontSize: '0.85rem', lineHeight: 1.5, maxHeight: '200px', overflowY: 'auto'}}>
                            {p.scraped_info}
                          </div>
                        </div>
                      )}

                      <div>
                        <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, fontSize: '0.85rem'}}>📝 Manual Notes</label>
                        <textarea className="form-input" rows={3} placeholder="Pricing, USPs, objection handling..."
                          defaultValue={p.manual_notes}
                          onBlur={e => handleSaveProduct(p.id, { manual_notes: e.target.value })}
                          style={{resize: 'vertical', minHeight: '70px', fontSize: '0.85rem'}} />
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* System Prompt Preview & Edit */}
          {selectedOrg && (
            <div className="glass-panel" style={{marginBottom: '2rem'}}>
              <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                <h4 style={{marginTop: 0, marginBottom: 0, fontSize: '1.1rem', fontWeight: 600}}>🤖 AI System Prompt</h4>
                <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                  {promptDirty && (
                    <button className="btn-primary" style={{background: 'linear-gradient(135deg, #10b981, #059669)', fontSize: '0.85rem', padding: '6px 14px'}}
                      onClick={handleSaveSystemPrompt} disabled={promptSaving}>
                      {promptSaving ? '⏳ Saving...' : '💾 Save Prompt'}
                    </button>
                  )}
                </div>
              </div>
              <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '1rem'}}>This is the product knowledge the AI receives during calls. Edit to customize what the AI knows.</p>

              {systemPromptAuto && !systemPromptCustom && (
                <div style={{marginBottom: '1rem'}}>
                  <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, color: '#22d3ee', fontSize: '0.85rem'}}>📄 Auto-Generated from Products</label>
                  <div style={{background: 'rgba(0,0,0,0.3)', padding: '12px', borderRadius: '8px',
                    border: '1px solid rgba(34, 211, 238, 0.15)', whiteSpace: 'pre-wrap',
                    color: '#cbd5e1', fontSize: '0.85rem', lineHeight: 1.6, maxHeight: '200px', overflowY: 'auto'}}>
                    {systemPromptAuto}
                  </div>
                </div>
              )}

              <div>
                <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, fontSize: '0.85rem'}}>✏️ Custom System Prompt {systemPromptCustom ? '(Active)' : '(Optional Override)'}</label>
                <textarea className="form-input" rows={8}
                  placeholder={systemPromptAuto || 'Add product info, scrape a website, then customize the prompt here...'}
                  value={systemPromptCustom}
                  onChange={e => { setSystemPromptCustom(e.target.value); setPromptDirty(true); }}
                  style={{resize: 'vertical', minHeight: '120px', fontSize: '0.85rem', lineHeight: 1.6}} />
                <p style={{color: '#64748b', fontSize: '0.75rem', marginTop: '6px'}}>If empty, the auto-generated version from your products is used. If you write a custom prompt, it overrides the auto-generated one.</p>
              </div>
            </div>
          )}
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

      {editModalOpen && editingLead && (
        <div className="modal-overlay" onClick={() => setEditModalOpen(false)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()}>
            <h2 style={{marginTop: 0, marginBottom: '2rem'}}>Edit Lead</h2>
            <form onSubmit={handleSaveEdit}>
              <div className="form-group">
                <label>First Name</label>
                <input className="form-input" required value={editFormData.first_name} onChange={e => setEditFormData({...editFormData, first_name: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Last Name <span style={{color: '#64748b', fontSize: '0.8rem'}}>(Optional)</span></label>
                <input className="form-input" value={editFormData.last_name} onChange={e => setEditFormData({...editFormData, last_name: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Phone Number</label>
                <input className="form-input" required type="tel" value={editFormData.phone} onChange={e => setEditFormData({...editFormData, phone: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Source</label>
                <input className="form-input" value={editFormData.source} onChange={e => setEditFormData({...editFormData, source: e.target.value})} />
              </div>
              <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '2.5rem'}}>
                <button type="button" className="btn-call" style={{borderColor: 'transparent', color: '#cbd5e1', background: 'transparent'}} onClick={() => setEditModalOpen(false)}>Cancel</button>
                <button type="submit" className="btn-primary" disabled={loading}>
                  {loading ? 'Saving...' : 'Update Lead'}
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

      {/* Call Transcript Modal */}
      {transcriptLead && (
        <div className="modal-overlay">
          <div className="modal-content glass-panel" style={{background: 'rgba(15, 23, 42, 0.97)', border: '1px solid rgba(99, 102, 241, 0.2)', maxWidth: '700px', maxHeight: '85vh', display: 'flex', flexDirection: 'column'}}>
            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem', borderBottom: '1px solid rgba(255,255,255,0.05)', paddingBottom: '1rem'}}>
              <div>
                <h2 style={{marginTop: 0, marginBottom: '4px', color: '#818cf8', display: 'flex', alignItems: 'center', gap: '8px'}}>📋 Call Transcripts</h2>
                <p style={{margin: 0, color: '#94a3b8', fontSize: '0.9rem'}}>{transcriptLead.first_name} — {transcriptLead.phone}</p>
              </div>
              <button className="btn-call" style={{borderColor: 'transparent', color: '#cbd5e1', background: 'transparent', fontSize: '1.2rem'}} onClick={() => setTranscriptLead(null)}>✕</button>
            </div>

            <div style={{flex: 1, overflowY: 'auto', paddingRight: '8px'}}>
              {transcripts.length === 0 ? (
                <div style={{padding: '3rem', textAlign: 'center', color: '#64748b', background: 'rgba(0,0,0,0.2)', borderRadius: '12px'}}>
                  <div style={{fontSize: '2rem', marginBottom: '12px'}}>📞</div>
                  <div>No call transcripts yet.</div>
                  <div style={{fontSize: '0.85rem', marginTop: '8px'}}>Transcripts will appear here after AI calls are completed.</div>
                </div>
              ) : (
                transcripts.map((t, idx) => (
                  <div key={t.id || idx} style={{marginBottom: '1.5rem', background: 'rgba(0,0,0,0.2)', borderRadius: '12px', padding: '1.25rem', border: '1px solid rgba(255,255,255,0.05)'}}>
                    <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                      <div style={{display: 'flex', alignItems: 'center', gap: '10px'}}>
                        <span style={{color: '#818cf8', fontWeight: 600}}>Call #{transcripts.length - idx}</span>
                        <span style={{fontSize: '0.8rem', color: '#64748b'}}>{new Date(t.created_at).toLocaleString()}</span>
                      </div>
                      {t.call_duration_s > 0 && (
                        <span className="badge" style={{background: 'rgba(99, 102, 241, 0.1)', color: '#818cf8', fontSize: '0.75rem'}}>{Math.round(t.call_duration_s)}s</span>
                      )}
                    </div>

                    {/* Audio Player */}
                    {t.recording_url && (
                      <div style={{marginBottom: '1rem', padding: '10px', background: 'rgba(99, 102, 241, 0.05)', borderRadius: '8px', border: '1px solid rgba(99, 102, 241, 0.15)'}}>
                        <div style={{fontSize: '0.8rem', color: '#818cf8', marginBottom: '6px', fontWeight: 600}}>🔊 Call Recording</div>
                        <audio controls style={{width: '100%', height: '36px'}} src={t.recording_url}>
                          Your browser does not support the audio element.
                        </audio>
                      </div>
                    )}

                    {/* Turn-by-turn transcript */}
                    <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
                      {(Array.isArray(t.transcript) ? t.transcript : []).map((turn, i) => (
                        <div key={i} style={{
                          display: 'flex',
                          flexDirection: turn.role === 'AI' ? 'row' : 'row-reverse',
                          gap: '8px',
                          alignItems: 'flex-start'
                        }}>
                          <div style={{
                            width: '28px', height: '28px', borderRadius: '50%', flexShrink: 0,
                            display: 'flex', alignItems: 'center', justifyContent: 'center',
                            fontSize: '0.75rem', fontWeight: 700,
                            background: turn.role === 'AI' ? 'rgba(99, 102, 241, 0.2)' : 'rgba(34, 197, 94, 0.2)',
                            color: turn.role === 'AI' ? '#818cf8' : '#4ade80',
                            border: `1px solid ${turn.role === 'AI' ? 'rgba(99, 102, 241, 0.3)' : 'rgba(34, 197, 94, 0.3)'}`
                          }}>
                            {turn.role === 'AI' ? '🤖' : '👤'}
                          </div>
                          <div style={{
                            maxWidth: '75%', padding: '10px 14px', borderRadius: '12px',
                            background: turn.role === 'AI' ? 'rgba(99, 102, 241, 0.08)' : 'rgba(34, 197, 94, 0.08)',
                            border: `1px solid ${turn.role === 'AI' ? 'rgba(99, 102, 241, 0.15)' : 'rgba(34, 197, 94, 0.15)'}`,
                            color: '#e2e8f0', fontSize: '0.9rem', lineHeight: '1.5'
                          }}>
                            <div style={{fontSize: '0.7rem', fontWeight: 600, marginBottom: '4px', color: turn.role === 'AI' ? '#818cf8' : '#4ade80'}}>
                              {turn.role === 'AI' ? 'Arjun (AI)' : transcriptLead.first_name || 'User'}
                            </div>
                            {turn.text}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))
              )}
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
