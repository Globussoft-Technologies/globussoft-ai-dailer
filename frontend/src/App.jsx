import React, { useState, useEffect, useRef } from 'react';
import CallMonitor from './CallMonitor';
import KnowledgeBase from './KnowledgeBase';
import Sandbox from './Sandbox';
import AuthPage from './components/AuthPage';
import TopHeader from './components/TopHeader';
import CrmTab from './components/tabs/CrmTab';
import OpsTab from './components/tabs/OpsTab';
import AnalyticsTab from './components/tabs/AnalyticsTab';
import WhatsAppTab from './components/tabs/WhatsAppTab';
import IntegrationsTab from './components/tabs/IntegrationsTab';
import SettingsTab from './components/tabs/SettingsTab';
import LogsTab from './components/tabs/LogsTab';
import CheckInTab from './components/tabs/CheckInTab';
import CampaignsTab from './components/tabs/CampaignsTab';
import LeadModals from './components/modals/LeadModals';
import DocumentVault from './components/modals/DocumentVault';
import TranscriptModal from './components/modals/TranscriptModal';
import EmailDraftModal from './components/modals/EmailDraftModal';
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

  const [campaigns, setCampaigns] = useState([]);

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
      { id: 'raj', name: 'Raj – Confident ♂' },
      { id: 'arnav', name: 'Arnav – Friendly ♂' },
      { id: 'raman', name: 'Raman – Natural ♂' },
      { id: 'raghav', name: 'Raghav – Professional ♂' },
      { id: 'aarav', name: 'Aarav – Calm ♂' },
      { id: 'ankur', name: 'Ankur – Relaxed ♂' },
      { id: 'aravind', name: 'Aravind – Narrative ♂' },
      { id: 'saurabh', name: 'Saurabh – Bold ♂' },
      { id: 'chetan', name: 'Chetan – Strong ♂' },
      { id: 'ashish', name: 'Ashish – Warm ♂' },
      { id: 'kajal', name: 'Kajal – Friendly ♀' },
      { id: 'pragya', name: 'Pragya – Upbeat ♀' },
      { id: 'nisha', name: 'Nisha – Kind ♀' },
      { id: 'deepika', name: 'Deepika – Bold ♀' },
      { id: 'diya', name: 'Diya – Young ♀' },
      { id: 'sushma', name: 'Sushma – Strong ♀' },
      { id: 'shweta', name: 'Shweta – Conversational ♀' },
      { id: 'ananya', name: 'Ananya – Narrative ♀' },
      { id: 'mithali', name: 'Mithali – Classic ♀' },
      { id: 'saina', name: 'Saina – Bold ♀' },
      { id: 'sanya', name: 'Sanya – Friendly ♀' },
      { id: 'pooja', name: 'Pooja – Informative ♀' },
      { id: 'mansi', name: 'Mansi – Narrative ♀' },
    ],
    sarvam: [
      { id: 'aditya', name: 'Aditya – Default ♂' },
      { id: 'rahul', name: 'Rahul – Conversational ♂' },
      { id: 'amit', name: 'Amit – Professional ♂' },
      { id: 'dev', name: 'Dev – Young ♂' },
      { id: 'rohan', name: 'Rohan – Friendly ♂' },
      { id: 'varun', name: 'Varun – Calm ♂' },
      { id: 'kabir', name: 'Kabir – Bold ♂' },
      { id: 'manan', name: 'Manan – Warm ♂' },
      { id: 'sumit', name: 'Sumit – Natural ♂' },
      { id: 'ratan', name: 'Ratan – Mature ♂' },
      { id: 'aayan', name: 'Aayan – Young ♂' },
      { id: 'shubh', name: 'Shubh – Energetic ♂' },
      { id: 'ashutosh', name: 'Ashutosh – Deep ♂' },
      { id: 'advait', name: 'Advait – Smooth ♂' },
      { id: 'ritu', name: 'Ritu – Warm ♀' },
      { id: 'priya', name: 'Priya – Friendly ♀' },
      { id: 'neha', name: 'Neha – Professional ♀' },
      { id: 'pooja', name: 'Pooja – Kind ♀' },
      { id: 'simran', name: 'Simran – Cheerful ♀' },
      { id: 'kavya', name: 'Kavya – Soft ♀' },
      { id: 'ishita', name: 'Ishita – Confident ♀' },
      { id: 'shreya', name: 'Shreya – Bright ♀' },
      { id: 'roopa', name: 'Roopa – Gentle ♀' },
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

  const fetchCampaigns = async () => {
    try { const res = await apiFetch(`${API_URL}/campaigns`); setCampaigns(await res.json()); } catch(e){}
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
    fetchCampaigns();
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

          // Upload whatever recording chunks we have
          const uploadRecording = async () => {
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

          if (mediaRecorder.state !== 'inactive') {
            mediaRecorder.stop();
            mediaRecorder.onstop = () => uploadRecording();
          } else {
            // MediaRecorder already stopped — upload whatever chunks we collected
            uploadRecording();
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

  const handleCampaignDial = async (lead, campaignId) => {
    setDialingId(lead.id);
    try {
      await apiFetch(`${API_URL}/campaigns/${campaignId}/dial/${lead.id}`, { method: "POST" });
    } catch(e) {}
    setTimeout(() => setDialingId(null), 3000);
  };

  const handleCampaignWebCall = async (lead, campaignId) => {
    if (webCallActive === lead.id) {
      if (webCallWsRef.current) webCallWsRef.current.close();
      if (webCallAudioCtxRef.current) webCallAudioCtxRef.current.close();
      setWebCallActive(null);
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const audioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 8000 });
      webCallAudioCtxRef.current = audioContext;

      const recDest = audioContext.createMediaStreamDestination();
      const mediaRecorder = new MediaRecorder(recDest.stream, { mimeType: 'audio/webm;codecs=opus' });
      const recordedChunks = [];
      mediaRecorder.ondataavailable = (e) => { if (e.data.size > 0) recordedChunks.push(e.data); };
      mediaRecorder.start(1000);

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
        campaign_id: String(campaignId),
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
        source.connect(recDest);

        let micMuted = true;
        let unmuteTimer = null;

        processor.onaudioprocess = (e) => {
          if (ws.readyState !== WebSocket.OPEN) return;
          if (micMuted) return;
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
            destSource.connect(recDest);

            if (audioContext.currentTime > nextPlayTime) nextPlayTime = audioContext.currentTime;
            destSource.start(nextPlayTime);
            nextPlayTime += buffer.duration;

            const remainingPlayMs = Math.max(0, (nextPlayTime - audioContext.currentTime) * 1000) + 500;
            unmuteTimer = setTimeout(() => { micMuted = false; }, remainingPlayMs);
          } else if (data.event === 'clear') {
            nextPlayTime = audioContext.currentTime;
            micMuted = false;
            if (unmuteTimer) clearTimeout(unmuteTimer);
          }
        };

        ws.onclose = () => {
          stream.getTracks().forEach(track => track.stop());

          const uploadRecording = async () => {
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

          if (mediaRecorder.state !== 'inactive') {
            mediaRecorder.stop();
            mediaRecorder.onstop = () => uploadRecording();
          } else {
            uploadRecording();
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
      const res = await apiFetch(`${API_URL}/leads/${editingLead.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(editFormData)
      });
      const data = await res.json();
      if (!res.ok || data.status === 'error') {
        throw new Error(data.message || 'Error updating lead details');
      }
      setEditModalOpen(false);
      setEditingLead(null);
      fetchLeads();
    } catch (e) {
      alert(e.message);
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
      <AuthPage 
        authPage={authPage} setAuthPage={setAuthPage}
        authError={authError} setAuthError={setAuthError}
        authLoading={authLoading}
        authForm={authForm} setAuthForm={setAuthForm}
        handleLogin={handleLogin} handleSignup={handleSignup}
      />
    );
  }

  return (
    <div className="dashboard-container">
      <TopHeader 
        activeTab={activeTab} setActiveTab={setActiveTab}
        userRole={userRole} currentUser={currentUser}
        handleLogout={handleLogout}
      />
      
      {activeTab === 'crm' ? (
        <CrmTab 
          searchQuery={searchQuery} handleSearch={handleSearch} setIsModalOpen={setIsModalOpen}
          userRole={userRole} leads={leads} API_URL={API_URL} authToken={authToken} fetchLeads={fetchLeads}
          activeVoiceProvider={activeVoiceProvider} setActiveVoiceProvider={setActiveVoiceProvider}
          activeVoiceId={activeVoiceId} setActiveVoiceId={setActiveVoiceId}
          activeLanguage={activeLanguage} setActiveLanguage={setActiveLanguage}
          INDIAN_VOICES={INDIAN_VOICES} INDIAN_LANGUAGES={INDIAN_LANGUAGES}
          selectedOrg={selectedOrg} apiFetch={apiFetch}
          savedVoiceName={savedVoiceName} setSavedVoiceName={setSavedVoiceName}
          handleStatusChange={handleStatusChange} handleEditLead={handleEditLead}
          handleDeleteLead={handleDeleteLead} handleOpenDocs={handleOpenDocs}
          handleViewTranscripts={handleViewTranscripts} handleNote={handleNote}
          handleDraftEmail={handleDraftEmail} dialingId={dialingId}
          webCallActive={webCallActive} handleWebCall={handleWebCall} handleDial={handleDial}
        />
      ) : activeTab === 'campaigns' ? (
        <CampaignsTab
          campaigns={campaigns} fetchCampaigns={fetchCampaigns}
          orgProducts={orgProducts} leads={leads}
          apiFetch={apiFetch} API_URL={API_URL} selectedOrg={selectedOrg}
          onCampaignDial={handleCampaignDial} onCampaignWebCall={handleCampaignWebCall}
          activeVoiceProvider={activeVoiceProvider} activeVoiceId={activeVoiceId}
          activeLanguage={activeLanguage} dialingId={dialingId} webCallActive={webCallActive}
        />
      ) : activeTab === 'ops' ? (
        <OpsTab reports={reports} tasks={tasks} handleCompleteTask={handleCompleteTask} />
      ) : activeTab === 'analytics' ? (
        <AnalyticsTab analyticsData={analyticsData} />
      ) : activeTab === 'whatsapp' ? (
        <WhatsAppTab whatsappLogs={whatsappLogs} />
      ) : activeTab === 'integrations' ? (
        <IntegrationsTab 
          handleCreateIntegration={handleCreateIntegration}
          intFormData={intFormData} setIntFormData={setIntFormData}
          CRM_SCHEMAS={CRM_SCHEMAS} loading={loading} integrations={integrations}
        />
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
        <SettingsTab 
          handleAddPronunciation={handleAddPronunciation} pronFormData={pronFormData}
          setPronFormData={setPronFormData} pronunciations={pronunciations}
          handleDeletePronunciation={handleDeletePronunciation} selectedOrg={selectedOrg}
          orgs={orgs} showProductInput={showProductInput} setShowProductInput={setShowProductInput}
          newProductName={newProductName} setNewProductName={setNewProductName}
          handleAddProduct={handleAddProduct} orgProducts={orgProducts}
          handleDeleteProduct={handleDeleteProduct} handleSaveProduct={handleSaveProduct}
          scraping={scraping} handleScrapeProduct={handleScrapeProduct}
          promptDirty={promptDirty} handleSaveSystemPrompt={handleSaveSystemPrompt}
          promptSaving={promptSaving} systemPromptAuto={systemPromptAuto}
          systemPromptCustom={systemPromptCustom} setSystemPromptCustom={setSystemPromptCustom}
          setPromptDirty={setPromptDirty}
        />
      ) : activeTab === 'logs' ? (
        <LogsTab API_URL={API_URL} authToken={authToken} />
      ) : (
        <CheckInTab 
          fieldOpsData={fieldOpsData} setFieldOpsData={setFieldOpsData}
          sites={sites} handlePunchIn={handlePunchIn} punching={punching}
          punchStatus={punchStatus}
        />
      )}

      <LeadModals 
        isModalOpen={isModalOpen} setIsModalOpen={setIsModalOpen}
        handleCreateLead={handleCreateLead} formData={formData}
        setFormData={setFormData} loading={loading}
        editModalOpen={editModalOpen} setEditModalOpen={setEditModalOpen}
        editingLead={editingLead} handleSaveEdit={handleSaveEdit}
        editFormData={editFormData} setEditFormData={setEditFormData}
      />
      <DocumentVault 
        activeLeadDocs={activeLeadDocs} setActiveLeadDocs={setActiveLeadDocs}
        handleUploadDoc={handleUploadDoc} docFormData={docFormData}
        setDocFormData={setDocFormData} docs={docs}
      />
      <TranscriptModal 
        transcriptLead={transcriptLead} setTranscriptLead={setTranscriptLead}
        transcripts={transcripts}
      />
      <EmailDraftModal 
        emailDraft={emailDraft} setEmailDraft={setEmailDraft}
      />
    </div>
  );
}

