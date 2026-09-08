import React, { useState, useEffect, useCallback } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useToast, useConfirm } from '../../contexts/UIContext';
import { useCampaign } from '../../hooks/useQueries';
import { queryClient } from '../../queryClient';
import CampaignDetail from '../campaigns/CampaignDetail';
import CampaignModals from '../campaigns/CampaignModals';
import { CAMPAIGN_TEMPLATES } from '../../constants/campaignTemplates';
import { useAuth } from '../../contexts/AuthContext';
import { normalizePhone } from '../../utils/phone';
import { isAdmin } from '../../utils/roles';

export default function CampaignsTab({
  campaigns, fetchCampaigns, orgProducts,
  apiFetch, API_URL,
  onCampaignDial, onCampaignWebCall,
  handleViewTranscripts,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  dialingId, webCallActive, orgTimezone
}) {
  const { fetchSseTicket, currentUser, hasPermission } = useAuth();
  const toast = useToast();
  const confirm = useConfirm();
  const location = useLocation();
  const navigate = useNavigate();
  const { campaignId: routeCampaignId } = useParams();
  const [view, setView] = useState('list'); // 'list' or 'detail'
  const [autoOpened, setAutoOpened] = useState(false);
  const [selectedCampaign, setSelectedCampaign] = useState(null);
  const [detailTab, setDetailTab] = useState('leads'); // 'leads' or 'calllog'
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showAddLeadsModal, setShowAddLeadsModal] = useState(false);
  const [editLead, setEditLead] = useState(null);
  const [editForm, setEditForm] = useState({ first_name: '', last_name: '', phone: '', company: '', source: '', executive_id: 0 });
  const [editErrors, setEditErrors] = useState({});
  const [createForm, setCreateForm] = useState({ name: '', product_id: '', lead_source: '', channel: 'voice', exotel_account_id: '', executive_ids: [] });
  const [orgExotelAccounts, setOrgExotelAccounts] = useState([]);
  const [executives, setExecutives] = useState([]);
  const [agents, setAgents] = useState([]);
  const [detailExecutiveFilter, setDetailExecutiveFilter] = useState([]);
  const [selectedLeadIds, setSelectedLeadIds] = useState([]);
  const [addLeadsSearch, setAddLeadsSearch] = useState('');
  const [addLeadsResults, setAddLeadsResults] = useState([]);
  const [addLeadsLoading, setAddLeadsLoading] = useState(false);
  const [loading, setLoading] = useState(false);
  const [showCsvImportModal, setShowCsvImportModal] = useState(false);
  const [csvFile, setCsvFile] = useState(null);
  const [csvImportResult, setCsvImportResult] = useState(null);
  const [liveEvents, setLiveEvents] = useState([]);
  const [showEditCampaignModal, setShowEditCampaignModal] = useState(false);
  const [editCampaignForm, setEditCampaignForm] = useState({ name: '', product_id: '', lead_source: '', executive_ids: [] });
  const [deleting, setDeleting] = useState(false);
  const [selectedTemplate, setSelectedTemplate] = useState(null);
  const [createError, setCreateError] = useState('');
  const [editCampaignError, setEditCampaignError] = useState('');
  const eventSourceRef = React.useRef(null);
  const [campVoice, setCampVoice] = useState({ tts_provider: '', tts_voice_id: '', tts_language: '' });
  const [campVoiceSaveStatus, setCampVoiceSaveStatus] = useState(''); // '', 'saving', 'saved', 'error'

  // Fetch campaign detail via React Query and merge into selectedCampaign.
  const { data: campaignDetail } = useCampaign(selectedCampaign?.id);
  /* eslint-disable react-hooks/exhaustive-deps */
  useEffect(() => {
    if (campaignDetail && selectedCampaign) {
      setSelectedCampaign(prev => (prev ? { ...prev, ...campaignDetail } : prev));
    }
  }, [campaignDetail]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // Campaign-user assignment state (Admin only).
  const [showAssignModal, setShowAssignModal] = useState(false);
  const [assignCampaign, setAssignCampaign] = useState(null);
  const [assignableUsers, setAssignableUsers] = useState([]);
  const [selectedUserIds, setSelectedUserIds] = useState([]);
  const [assignLoading, setAssignLoading] = useState(false);

  useEffect(() => {
    fetchCampaigns();
    apiFetch(`${API_URL}/exotel-accounts/options`)
      .then(r => r.ok ? r.json() : [])
      .then(d => setOrgExotelAccounts(Array.isArray(d) ? d : []))
      .catch(() => {});
    apiFetch(`${API_URL}/executives`)
      .then(r => r.json())
      .then(d => setExecutives(Array.isArray(d) ? d : []))
      .catch(() => {});
  }, [apiFetch, API_URL, fetchCampaigns]);

  useEffect(() => {
    if (!isAdmin(currentUser?.role)) {
      setAgents([]);
      setDetailExecutiveFilter([]);
      return;
    }
    apiFetch(`${API_URL}/team`)
      .then(r => r.ok ? r.json() : [])
      .then(d => {
        const team = Array.isArray(d) ? d : [];
        setAgents(team.filter(u => u.role === 'Agent'));
      })
      .catch(() => setAgents([]));
  }, [apiFetch, API_URL, currentUser?.role]);

  // Open a specific campaign's detail directly when ?id=N is in the URL —
  // lets the CRM dashboard's "Active Campaigns" cards navigate straight into
  // the right campaign instead of dropping the user on the list (issue #40).
  // Runs whenever the campaigns array refreshes so we can resolve the id
  // once the list has loaded; clears the param afterwards so a Back-to-list
  // doesn't keep re-opening the same campaign.
  useEffect(() => {
    if (view === 'detail') return;
    if (!Array.isArray(campaigns) || campaigns.length === 0) return;
    const params = new URLSearchParams(window.location.search);
    const idStr = params.get('id');
    if (!idStr) return;
    const id = parseInt(idStr, 10);
    if (!Number.isFinite(id)) return;
    const target = campaigns.find(c => c.id === id);
    if (!target) return;
    handleViewCampaign(target);
    // Strip ?id= from the URL so refreshes / Back don't loop.
    window.history.replaceState({}, '', window.location.pathname);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [campaigns, view]);

  // Auto-open a specific campaign when navigated from the CRM dashboard.
  // After opening, clear the navigation state so that clicking "Back to Campaigns"
  // shows the list without re-triggering this effect.
  /* eslint-disable react-hooks/exhaustive-deps */
  useEffect(() => {
    const openId = location.state?.openCampaignId;
    if (!openId || !campaigns?.length) return;
    const target = campaigns.find(c => c.id === openId);
    if (target) {
      handleViewCampaign(target);
      navigate('/campaigns', { replace: true, state: {} });
    }
  }, [location.state?.openCampaignId, campaigns]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // If the user leaves the /campaigns/:campaignId route (Back button or manual URL change),
  // reset to list view so the same component instance doesn't keep showing the detail.
  /* eslint-disable react-hooks/exhaustive-deps */
  useEffect(() => {
    if (!routeCampaignId && view === 'detail') {
      stopEventStream();
      setAutoOpened(false);
      setView('list');
      setSelectedCampaign(null);
      setLiveEvents([]);
      fetchCampaigns();
    }
  }, [routeCampaignId, view]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // Close any active SSE stream when this component unmounts.
  useEffect(() => () => stopEventStream(), []);

  // Reset add-leads search state when the modal closes.
  useEffect(() => {
    if (!showAddLeadsModal) {
      setAddLeadsSearch('');
      setAddLeadsResults([]);
      setSelectedLeadIds([]);
    }
  }, [showAddLeadsModal]);

  // Auto-open the campaign from the /campaigns/:campaignId route.
  // Using autoOpened prevents the list view from flashing and stops repeated attempts.
  useEffect(() => {
    if (!routeCampaignId || autoOpened) return;
    const id = parseInt(routeCampaignId, 10);
    if (!campaigns?.length) return;
    const target = campaigns.find(c => c.id === id);
    if (target) {
      setAutoOpened(true);
      setSelectedCampaign(target);
      setView('detail');
      setDetailExecutiveFilter([]);
      fetchCampVoice(target.id);
      startEventStream(target.id).catch(() => {});
      setDetailTab('leads');
    } else {
      setAutoOpened(true);
      navigate('/campaigns', { replace: true });
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [routeCampaignId, campaigns, autoOpened]);

  const fetchCampaignLeads = useCallback((campaignId) => {
    queryClient.invalidateQueries({ queryKey: ['campaign', campaignId, 'leads'] });
  }, []);

  const fetchCallLog = useCallback((campaignId) => {
    queryClient.invalidateQueries({ queryKey: ['campaign', campaignId, 'callLogs'] });
  }, []);

  const fetchCampaignDetail = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}`);
      if (res.ok) return await res.json();
    } catch { /* ignore */ }
    return null;
  };

  const fetchCampVoice = async (campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/voice-settings`);
      if (res.ok) {
        const data = await res.json();
        setCampVoice({ tts_provider: data.tts_provider || '', tts_voice_id: data.tts_voice_id || '', tts_language: data.tts_language || '' });
      } else {
        setCampVoice({ tts_provider: '', tts_voice_id: '', tts_language: '' });
      }
    } catch { setCampVoice({ tts_provider: '', tts_voice_id: '', tts_language: ''  }); }
  };

  const handleSaveCampVoice = async () => {
    if (!selectedCampaign) return;
    setCampVoiceSaveStatus('saving');
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/voice-settings`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tts_provider: campVoice.tts_provider, tts_voice_id: campVoice.tts_voice_id, tts_language: campVoice.tts_language })
      });
      if (!res.ok) {
        setCampVoiceSaveStatus('error');
        setTimeout(() => setCampVoiceSaveStatus(''), 3000);
        return;
      }
      setCampVoiceSaveStatus('saved');
      setTimeout(() => setCampVoiceSaveStatus(''), 2000);
    } catch { setCampVoiceSaveStatus('error');
      setTimeout(() => setCampVoiceSaveStatus(''), 3000);
     }
  };

  const handleResetCampVoice = async () => {
    if (!selectedCampaign) return;
    await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/voice-settings`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tts_provider: '', tts_voice_id: '', tts_language: '' })
    });
    setCampVoice({ tts_provider: '', tts_voice_id: '', tts_language: '' });
  };

  const handleViewCampaign = async (campaign) => {
    if (routeCampaignId && campaign?.id !== parseInt(routeCampaignId, 10)) {
      navigate(`/campaigns/${campaign.id}`, { replace: true });
    }
    setDetailExecutiveFilter([]);
    setSelectedCampaign(campaign);
    setView('detail');
    fetchCampVoice(campaign.id);
    startEventStream(campaign.id).catch(() => {});
    setDetailTab('leads');
  };

  const handleBack = () => {
    stopEventStream();
    setAutoOpened(false);
    setView('list');
    setSelectedCampaign(null);
    setLiveEvents([]);
    if (routeCampaignId) {
      navigate('/campaigns');
      return;
    }
    fetchCampaigns();
  };

  const startEventStream = async (campaignId) => {
    stopEventStream();
    let ticket;
    try { ticket = await fetchSseTicket(); } catch { return;  }
    const es = new EventSource(`${API_URL}/campaign-events?ticket=${encodeURIComponent(ticket)}&campaign_id=${campaignId}`);
    es.onmessage = (e) => {
      // Backend publishes a JSON envelope with a pre-formatted `label` field;
      // legacy events arrive as plain strings, so fall back to the raw line
      // when JSON parse fails or no label is present.
      let display = e.data;
      let ts = Date.now();
      try {
        const j = JSON.parse(e.data);
        if (j && typeof j.label === 'string') display = j.label;
        if (j && j.ts) {
          const parsed = new Date(j.ts).getTime();
          if (!Number.isNaN(parsed)) ts = parsed;
        }
      } catch { /* plain-text legacy event */  }
      // Drop replayed events older than the user's last Clear timestamp for
      // this campaign — the backend replays the last 20 events from Redis on
      // every SSE connect, so without this filter a page reload would
      // resurrect everything the user just cleared.
      const clearedAt = parseInt(localStorage.getItem(`liveEventsClearedAt:${campaignId}`) || '0', 10);
      if (clearedAt > 0 && ts <= clearedAt) return;
      setLiveEvents(prev => [...prev.slice(-49), { ts, label: display }]);
    };
    // Don't call es.close() here — that prevents EventSource's built-in
    // auto-reconnect. Cloudflare/nginx idle-timeout the SSE stream after
    // ~30s of silence; we want the browser to transparently re-open so new
    // call events still appear in the panel after the user clicks Clear or
    // simply waits idle for a while.
    es.onerror = (e) => {
      // Native EventSource will set readyState to CLOSED only when the
      // server explicitly returns a non-200; CONNECTING means a retry is
      // already in flight. Just log so we can see it in DevTools.
       
      console.warn('campaign-events SSE error; readyState=', es.readyState, e);
    };
    eventSourceRef.current = es;
  };

  const stopEventStream = () => {
    if (eventSourceRef.current) { eventSourceRef.current.close(); eventSourceRef.current = null; }
  };

  const handleCreateCampaign = async (e) => {
    e.preventDefault();
    if (!createForm.name.trim()) return;
    setLoading(true);
    setCreateError('');
    try {
      const res = await apiFetch(`${API_URL}/campaigns`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: createForm.name.trim(),
          product_id: createForm.product_id ? parseInt(createForm.product_id) : null,
          lead_source: createForm.lead_source || null,
          channel: createForm.channel || 'voice',
          exotel_account_id: createForm.exotel_account_id ? parseInt(createForm.exotel_account_id) : null,
        })
      });

      let newCampaign;
      try { newCampaign = await res.json(); } catch { newCampaign = {}; }

      if (!res.ok) {
        const msg = newCampaign?.detail || `Server error (${res.status})`;
        setCreateError(typeof msg === 'string' ? msg : JSON.stringify(msg));
        return;
      }

      // Apply template settings if one was selected
      if (selectedTemplate && newCampaign?.id) {
        await apiFetch(`${API_URL}/campaigns/${newCampaign.id}/voice-settings`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            tts_provider: selectedTemplate.tts_provider,
            tts_voice_id: selectedTemplate.tts_voice_id,
            tts_language: selectedTemplate.language
          })
        });

        const productId = createForm.product_id || newCampaign.product_id;
        if (productId) {
          await apiFetch(`${API_URL}/products/${productId}/prompt`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              agent_persona: selectedTemplate.agent_persona,
              call_flow_instructions: selectedTemplate.call_flow_instructions
            })
          });
        }
      }

      setCreateForm({ name: '', product_id: '', lead_source: '', channel: 'voice', exotel_account_id: '', executive_ids: [] });
      setSelectedTemplate(null);
      setCreateError('');
      setShowCreateModal(false);
      fetchCampaigns();
    } catch (err) {
      console.error(err);
      setCreateError('Network error — please try again.');
    } finally {
      setLoading(false);
    }
  };

  const confirmDeleteCampaign = async (campaignId, campaignName) => {
    if (deleting) return;
    const ok = await confirm({
      message: `Delete "${campaignName || 'this campaign'}"? This will permanently remove the campaign and its associations. This action cannot be undone.`,
      danger: true,
    });
    if (!ok) return;
    setDeleting(true);
    try {
      await apiFetch(`${API_URL}/campaigns/${campaignId}`, { method: 'DELETE' });
      fetchCampaigns();
    } catch (e) {
      console.error(e);
    } finally {
      setDeleting(false);
    }
  };

  const handleEditCampaign = async (campaign) => {
    const detail = await fetchCampaignDetail(campaign.id);
    const c = detail ? { ...campaign, ...detail } : campaign;
    setEditCampaignForm({
      id: c.id,
      name: c.name || '',
      product_id: c.product_id || '',
      lead_source: c.lead_source || '',
      channel: c.channel || 'voice',
      executive_ids: (c.executive_ids || []).map(id => String(id))
    });
    setEditCampaignError('');
    setShowEditCampaignModal(true);
  };

  const handleSaveEditCampaign = async (e) => {
    e.preventDefault();
    if (!editCampaignForm.name.trim()) { setEditCampaignError('Campaign name is required.'); return; }
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/campaigns/${editCampaignForm.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: editCampaignForm.name.trim(),
          product_id: editCampaignForm.product_id ? parseInt(editCampaignForm.product_id) : null,
          lead_source: editCampaignForm.lead_source || null,
          channel: editCampaignForm.channel || 'voice'
        })
      });
      setShowEditCampaignModal(false);
      fetchCampaigns();
      if (selectedCampaign?.id === editCampaignForm.id) {
        setSelectedCampaign(prev => ({
          ...prev,
          name: editCampaignForm.name.trim(),
          product_id: editCampaignForm.product_id ? parseInt(editCampaignForm.product_id) : prev.product_id,
          lead_source: editCampaignForm.lead_source || null,
          channel: editCampaignForm.channel || 'voice'
        }));
      }
      toast('Campaign updated');
    } catch (e) { console.error(e); toast('Failed to update campaign', 'error'); }
    setLoading(false);
  };

  // ── Campaign-user assignment (Admin only) ─────────────────────────────────
  const userRole = currentUser?.role || 'Agent';
  const canViewCampaigns = hasPermission('campaigns.view');
  const canCreateCampaigns = hasPermission('campaigns.create');
  const canEditCampaigns = hasPermission('campaigns.edit');
  const canDeleteCampaigns = hasPermission('campaigns.delete');

  const openAssignModal = async (campaign) => {
    setAssignCampaign(campaign);
    setShowAssignModal(true);
    setAssignLoading(true);
    try {
      const [usersRes, assignedRes] = await Promise.all([
        apiFetch(`${API_URL}/users`),
        apiFetch(`${API_URL}/campaigns/${campaign.id}/assigned-users`),
      ]);
      let users = [];
      if (usersRes.ok) {
        const allUsers = await usersRes.json();
        users = (allUsers || []).filter(u => u.role === 'Agent' || u.role === 'Executive' || u.role === 'TeamLeader');
      }
      setAssignableUsers(users);
      if (assignedRes.ok) {
        const data = await assignedRes.json();
        setSelectedUserIds(Array.isArray(data.user_ids) ? data.user_ids : []);
      } else {
        setSelectedUserIds([]);
      }
    } catch (e) {
      console.error(e);
      toast('Failed to load users', 'error');
    }
    setAssignLoading(false);
  };

  const closeAssignModal = () => {
    setShowAssignModal(false);
    setAssignCampaign(null);
    setAssignableUsers([]);
    setSelectedUserIds([]);
  };

  const toggleUserSelection = (userId) => {
    setSelectedUserIds(prev =>
      prev.includes(userId) ? prev.filter(id => id !== userId) : [...prev, userId]
    );
  };

  const handleAssignUsers = async () => {
    if (!assignCampaign) return;
    setAssignLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${assignCampaign.id}/assign-users`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_ids: selectedUserIds }),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        toast('Users assigned successfully', 'success');
        closeAssignModal();
      } else {
        toast(data.error || 'Failed to assign users', 'error');
      }
    } catch (e) {
      console.error(e);
      toast('Network error', 'error');
    }
    setAssignLoading(false);
  };

  const handleAddLeads = async () => {
    if (selectedLeadIds.length === 0) return;
    setLoading(true);
    try {
      await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lead_ids: selectedLeadIds })
      });
      setSelectedLeadIds([]);
      setAddLeadsSearch('');
      setAddLeadsResults([]);
      setShowAddLeadsModal(false);
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
    } catch (e) { console.error(e); }
    setLoading(false);
  };

  const searchAvailableLeads = useCallback(async (q) => {
    if (!selectedCampaign?.id) return;
    setAddLeadsSearch(q);
    const query = q.trim();
    if (query.length < 2) {
      setAddLeadsResults([]);
      return;
    }
    setAddLeadsLoading(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/search?q=${encodeURIComponent(query)}&exclude_campaign_id=${selectedCampaign.id}`);
      if (res.ok) {
        const data = await res.json();
        setAddLeadsResults(Array.isArray(data) ? data : []);
      } else {
        setAddLeadsResults([]);
      }
    } catch { setAddLeadsResults([]); }
    setAddLeadsLoading(false);
  }, [selectedCampaign?.id, apiFetch, API_URL]);

  const handleRemoveLead = async (leadId) => {
    try {
      await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads/${leadId}`, { method: 'DELETE' });
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
    } catch (e) { console.error(e); }
  };

  const handleDeleteLead = async (leadId) => {
    try {
      const res = await apiFetch(`${API_URL}/leads/${leadId}`, { method: 'DELETE' });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        toast(data.error || `Delete failed (${res.status})`, 'error');
        return;
      }
      fetchCampaignLeads(selectedCampaign.id);
      fetchCampaigns();
      toast('Lead deleted');
    } catch (e) { console.error(e); toast('Delete failed', 'error'); }
  };

  const handleEditLead = (lead) => {
    setEditLead(lead);
    setEditForm({ first_name: lead.first_name || '', last_name: lead.last_name || '', phone: lead.phone || '', company: lead.company || '', source: lead.source || '', executive_id: lead.executive_id || 0 });
    setEditErrors({});
  };

  const handleSaveEdit = async () => {
    if (!editLead) return;
    const payload = {
      first_name: editForm.first_name,
      last_name: editForm.last_name,
      phone: normalizePhone(editForm.phone),
      company: editForm.company,
      source: editForm.source,
      interest: '',
      executive_id: editForm.executive_id ? parseInt(editForm.executive_id, 10) : 0
    };
    try {
      const res = await apiFetch(`${API_URL}/leads/${editLead.id}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        setEditLead(null);
        fetchCampaignLeads(selectedCampaign.id);
        return;
      }
      const data = await res.json().catch(() => ({}));
      const errMsg = data.error || data.message || '';
      if (data.fields) {
        setEditErrors(data.fields);
        toast(errMsg || 'validation failed');
        return;
      }
      const isDuplicate = res.status === 409 || errMsg.includes('already exists') || (data.fields && data.fields.phone);
      if (isDuplicate) {
        const searchRes = await apiFetch(`${API_URL}/leads/search?q=${encodeURIComponent(editForm.phone)}`);
        const found = await searchRes.json();
        const existing = Array.isArray(found) ? found.find(l => l.phone === editForm.phone) : null;
        if (!existing) {
          toast('A lead with this phone already exists but could not be located.');
          return;
        }
        if (existing.id === editLead.id) {
          setEditLead(null);
          fetchCampaignLeads(selectedCampaign.id);
          return;
        }
        await apiFetch(`${API_URL}/leads/${existing.id}`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ lead_ids: [existing.id] })
        });
        await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads/${editLead.id}`, { method: 'DELETE' });
        setEditLead(null);
        fetchCampaignLeads(selectedCampaign.id);
        toast('Merged with existing lead.');
        return;
      }
      toast(errMsg || 'Save failed');
    } catch { toast('Save failed'); }
  };

  const handleLeadStatusChange = async (leadId, newStatus) => {
    try {
      await apiFetch(`${API_URL}/leads/${leadId}/status`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: newStatus })
      });
      fetchCampaignLeads(selectedCampaign.id);
    } catch (e) { console.error(e); }
  };

  const toggleLeadSelection = (leadId) => {
    setSelectedLeadIds(prev =>
      prev.includes(leadId) ? prev.filter(id => id !== leadId) : [...prev, leadId]
    );
  };

  const statusBadge = (status) => {
    const colors = { active: '#22c55e', paused: '#eab308', completed: '#6b7280' };
    const bg = { active: 'rgba(34,197,94,0.15)', paused: 'rgba(234,179,8,0.15)', completed: 'rgba(107,114,128,0.15)' };
    return (
      <span style={{
        padding: '2px 10px', borderRadius: '12px', fontSize: '0.75rem', fontWeight: 600,
        color: colors[status] || '#94a3b8', background: bg[status] || 'rgba(148,163,184,0.15)'
      }}>
        {status}
      </span>
    );
  };

  const getProductName = (productId) => {
    const p = orgProducts.find(p => p.id === productId);
    return p ? p.name : '';
  };

  const getCampaignStats = (campaign) => {
    const s = campaign.stats || {};
    const total = s.total || 0;
    const called = s.called || 0;
    return {
      total,
      called,
      remaining: Math.max(0, total - called),
      qualified: s.qualified || 0,
      booked: s.appointments || 0,
    };
  };

  const handleCsvImport = async () => {
    if (!csvFile || !selectedCampaign) return;
    setLoading(true);
    setCsvImportResult(null);
    try {
      const formData = new FormData();
      formData.append('file', csvFile);
      const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/import-csv`, {
        method: 'POST', body: formData
      });
      const data = await res.json();
      if (!res.ok) {
        setCsvImportResult({ error: data.error || 'Import failed' });
        toast(data.error || 'Import failed', 'error');
      } else {
        const rejectedCount = Array.isArray(data.rejected) ? data.rejected.length : 0;
        const errCount = Array.isArray(data.errors) ? data.errors.length : 0;
        const hasIssues = rejectedCount > 0 || errCount > 0;
        setCsvImportResult(data);
        toast(`Imported ${data.imported || 0} leads, ${data.added_to_campaign || 0} added to campaign.${hasIssues ? ` ${rejectedCount} rejected.` : ''}`);
        setCsvFile(null);
        fetchCampaignLeads(selectedCampaign.id);
        fetchCampaigns();
        if (!hasIssues) {
          setShowCsvImportModal(false);
          setCsvImportResult(null);
        }
      }
    } catch (e) { console.error(e); setCsvImportResult({ error: 'Import failed' }); toast('Import failed', 'error'); }
    setLoading(false);
  };

  const closeCsvImportModal = () => {
    setShowCsvImportModal(false);
    setCsvImportResult(null);
    setCsvFile(null);
  };

  // ─── DETAIL VIEW ───
  if (view === 'detail' && selectedCampaign) {
    return (
      <>
        <CampaignDetail
          selectedCampaign={selectedCampaign}
          setSelectedCampaign={setSelectedCampaign}
          campaignLeads={[]}
          campaignLeadsTotal={0}
          callLog={[]}
          detailTab={detailTab}
          setDetailTab={setDetailTab}
          handleBack={handleBack}
          fetchCampaignLeads={fetchCampaignLeads}
          fetchCallLog={fetchCallLog}
          fetchCampaigns={fetchCampaigns}
          statusBadge={statusBadge}
          getProductName={getProductName}
          getCampaignStats={getCampaignStats}
          campVoice={campVoice}
          setCampVoice={setCampVoice}
          handleSaveCampVoice={handleSaveCampVoice}
          handleResetCampVoice={handleResetCampVoice}
          campVoiceSaveStatus={campVoiceSaveStatus}
          INDIAN_VOICES={INDIAN_VOICES}
          INDIAN_LANGUAGES={INDIAN_LANGUAGES}
          liveEvents={liveEvents}
          setLiveEvents={setLiveEvents}
          handleLeadStatusChange={handleLeadStatusChange}
          handleEditLead={handleEditLead}
          handleRemoveLead={handleRemoveLead}
          handleDeleteLead={handleDeleteLead}
          handleViewTranscripts={handleViewTranscripts}
          onCampaignDial={onCampaignDial}
          onCampaignWebCall={onCampaignWebCall}
          dialingId={dialingId}
          webCallActive={webCallActive}
          setSelectedLeadIds={setSelectedLeadIds}
          setShowAddLeadsModal={setShowAddLeadsModal}
          setShowCsvImportModal={setShowCsvImportModal}
          setCsvFile={setCsvFile}
          apiFetch={apiFetch}
          API_URL={API_URL}
          orgTimezone={orgTimezone}
          handleEditCampaign={handleEditCampaign}
          executives={executives}
          agents={agents}
          detailExecutiveFilter={detailExecutiveFilter}
          setDetailExecutiveFilter={setDetailExecutiveFilter}
        />
        <CampaignModals
          showCreateModal={false}
          setShowCreateModal={setShowCreateModal}
          createForm={createForm}
          setCreateForm={setCreateForm}
          handleCreateCampaign={handleCreateCampaign}
          loading={loading}
          orgProducts={orgProducts}
          orgExotelAccounts={orgExotelAccounts}
          executives={executives}
          selectedTemplate={selectedTemplate}
          setSelectedTemplate={setSelectedTemplate}
          showAddLeadsModal={showAddLeadsModal}
          setShowAddLeadsModal={setShowAddLeadsModal}
          addLeadsSearch={addLeadsSearch}
          addLeadsResults={addLeadsResults}
          addLeadsLoading={addLeadsLoading}
          searchAvailableLeads={searchAvailableLeads}
          selectedLeadIds={selectedLeadIds}
          toggleLeadSelection={toggleLeadSelection}
          handleAddLeads={handleAddLeads}
          showCsvImportModal={showCsvImportModal}
          setShowCsvImportModal={setShowCsvImportModal}
          csvFile={csvFile}
          setCsvFile={setCsvFile}
          handleCsvImport={handleCsvImport}
          csvImportResult={csvImportResult}
          setCsvImportResult={setCsvImportResult}
          closeCsvImportModal={closeCsvImportModal}
          editLead={editLead}
          setEditLead={setEditLead}
          editForm={editForm}
          setEditForm={setEditForm}
          handleSaveEdit={handleSaveEdit}
          editErrors={editErrors}
          setEditErrors={setEditErrors}
          showEditCampaignModal={showEditCampaignModal}
          setShowEditCampaignModal={setShowEditCampaignModal}
          editCampaignForm={editCampaignForm}
          setEditCampaignForm={setEditCampaignForm}
          handleSaveEditCampaign={handleSaveEditCampaign}
          editCampaignError={editCampaignError}
          setEditCampaignError={setEditCampaignError}
        />
      </>
    );
  }

  // ─── LIST VIEW ───
  if (routeCampaignId && view !== 'detail' && !autoOpened) {
    return (
      <div style={{ padding: '28px 32px', background: '#f4f5f9', minHeight: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <div style={{ color: '#6b7280', fontSize: 14 }}>Loading campaign…</div>
      </div>
    );
  }

  const cardStyle = {
    background: '#fff', border: '1px solid #e5e7eb',
    borderRadius: 14, boxShadow: '0 1px 4px rgba(0,0,0,0.05)',
    padding: '20px 22px', display: 'flex', flexDirection: 'column', gap: 12,
  };
  const smallBtn = (bg, color, border) => ({
    padding: '5px 14px', borderRadius: 8, border: `1px solid ${border}`,
    background: bg, color, fontSize: 12, fontWeight: 600,
    cursor: 'pointer', fontFamily: 'inherit',
  });
  const browserProviderLabel = (campaign) => {
    if (campaign.channel === 'whatsapp') return 'WhatsApp';
    const accountId = campaign.exotel_account_id || campaign.exotelAccountId;
    if (!accountId) return 'Org default';
    const account = orgExotelAccounts.find(a => String(a.id) === String(accountId));
    if (!account) return `Account #${accountId}`;
    const rawProvider = account.provider || 'Exotel';
    const provider = String(rawProvider).charAt(0).toUpperCase() + String(rawProvider).slice(1);
    const name = account.name || account.account_sid || `Account #${account.id}`;
    return `${provider} · ${name}${account.caller_id ? ` · ${account.caller_id}` : ''}`;
  };

  if (!canViewCampaigns) {
    return (
      <div style={{ padding: '28px 32px', background: '#f4f5f9', minHeight: '100%' }}>
        <div style={{ ...cardStyle, textAlign: 'center', padding: '3rem', color: '#9ca3af' }}>
          You do not have permission to view campaigns.
        </div>
      </div>
    );
  }

  return (
    <div style={{ padding: '28px 32px', background: '#f4f5f9', minHeight: '100%' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h2 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: '#111827', display: 'flex', alignItems: 'center', gap: 10 }}>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#6366f1" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 010 7.07"/></svg>
          Campaigns
        </h2>
        {canCreateCampaigns && (
          <button
            onClick={() => setShowCreateModal(true)}
            style={{ background: '#6366f1', border: 'none', color: '#fff', borderRadius: 8, padding: '8px 20px', fontWeight: 700, fontSize: 13, cursor: 'pointer', fontFamily: 'inherit' }}>
            + Create Campaign
          </button>
        )}
      </div>

      {campaigns.length === 0 ? (
        <div style={{ ...cardStyle, textAlign: 'center', padding: '3rem', color: '#9ca3af' }}>
          No campaigns yet. Create one to start dialing!
        </div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 16 }}>
          {campaigns.map(campaign => {
            const stats = getCampaignStats(campaign);
            const calledPct = stats.total > 0 ? Math.round((stats.called / stats.total) * 100) : 0;
            const typeColor = campaign.channel === 'whatsapp' ? '#25D366' : '#6366f1';
            return (
              <div key={campaign.id} style={cardStyle}>
                {/* Card header: name + edit/delete */}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <div style={{ fontSize: 15, fontWeight: 700, color: '#111827', wordBreak: 'break-word', flex: 1, marginRight: 10 }}>
                    {campaign.name}
                  </div>
                  <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                    {isAdmin(userRole) && canEditCampaigns && (
                      <button onClick={(e) => { e.stopPropagation(); openAssignModal(campaign); }}
                        style={smallBtn('#eff6ff', '#2563eb', '#bfdbfe')}>
                        Assign Users
                      </button>
                    )}
                    {canEditCampaigns && (
                      <button onClick={(e) => { e.stopPropagation(); handleEditCampaign(campaign); }}
                        style={smallBtn('#fff', '#374151', '#e5e7eb')}>
                        Edit
                      </button>
                    )}
                    {canDeleteCampaigns && (
                      <button onClick={(e) => { e.stopPropagation(); confirmDeleteCampaign(campaign.id, campaign.name); }}
                        disabled={deleting}
                        style={smallBtn('#fee2e2', '#ef4444', '#fca5a5')}>
                        {deleting ? '…' : 'Delete'}
                      </button>
                    )}
                  </div>
                </div>

                {/* Badges */}
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  <span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: typeColor, background: `${typeColor}18` }}>
                    {campaign.channel === 'whatsapp' ? 'WhatsApp' : 'Voice'}
                  </span>
                  {campaign.product_id > 0 ? (
                    <span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: '#0891b2', background: 'rgba(8,145,178,0.1)' }}>
                      {getProductName(campaign.product_id)}
                    </span>
                  ) : (
                    <span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: '#f59e0b', background: 'rgba(245,158,11,0.1)' }}>
                      ⚠ No product
                    </span>
                  )}
                  {statusBadge(campaign.status || 'active')}
                </div>

                <div style={{ fontSize: 12, color: '#6b7280', lineHeight: 1.35 }}>
                  <span style={{ fontWeight: 700, color: '#374151' }}>Browser Provider:</span> {browserProviderLabel(campaign)}
                </div>

                {/* Stats */}
                <div style={{ display: 'flex', gap: 24 }}>
                  {[
                    { label: 'Total',     val: stats.total,     color: '#111827' },
                    { label: 'Called',    val: stats.called,    color: '#111827' },
                    { label: 'Remaining', val: stats.remaining, color: '#f59e0b' },
                    { label: 'Qualified', val: stats.qualified, color: '#10b981' },
                    { label: 'Booked',    val: stats.booked,    color: '#6366f1' },
                  ].map(({ label, val, color }) => (
                    <div key={label}>
                      <span style={{ fontSize: 12, color: '#9ca3af' }}>{label}: </span>
                      <span style={{ fontSize: 13, fontWeight: 700, fontFamily: "'DM Mono', monospace", color: val === 0 ? '#9ca3af' : color }}>{val}</span>
                    </div>
                  ))}
                </div>

                {/* Progress bar */}
                <div style={{ height: 5, background: '#e5e7eb', borderRadius: 3, overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: `${calledPct}%`, background: 'linear-gradient(90deg, #6366f1, #ec4899)', borderRadius: 3, transition: 'width 0.4s' }} />
                </div>

                {/* View Leads button */}
                <div>
                  <button onClick={() => navigate(`/campaigns/${campaign.id}`)}
                    style={{ background: 'none', border: 'none', color: '#9ca3af', cursor: 'pointer', fontSize: 13, fontWeight: 600, padding: 0, fontFamily: 'inherit' }}>
                    View Leads →
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <CampaignModals
        showCreateModal={showCreateModal}
        setShowCreateModal={setShowCreateModal}
        createForm={createForm}
        setCreateForm={setCreateForm}
        handleCreateCampaign={handleCreateCampaign}
        loading={loading}
        createError={createError}
        setCreateError={setCreateError}
        orgProducts={orgProducts}
        orgExotelAccounts={orgExotelAccounts}
        executives={executives}
        selectedTemplate={selectedTemplate}
        setSelectedTemplate={setSelectedTemplate}
        showAddLeadsModal={false}
        setShowAddLeadsModal={setShowAddLeadsModal}
        selectedLeadIds={selectedLeadIds}
        toggleLeadSelection={toggleLeadSelection}
        handleAddLeads={handleAddLeads}
        showCsvImportModal={false}
        setShowCsvImportModal={setShowCsvImportModal}
        csvFile={csvFile}
        setCsvFile={setCsvFile}
        handleCsvImport={handleCsvImport}
        csvImportResult={csvImportResult}
        setCsvImportResult={setCsvImportResult}
        closeCsvImportModal={closeCsvImportModal}
        editLead={null}
        setEditLead={setEditLead}
        editForm={editForm}
        setEditForm={setEditForm}
        handleSaveEdit={handleSaveEdit}
        showEditCampaignModal={showEditCampaignModal}
        setShowEditCampaignModal={setShowEditCampaignModal}
        editCampaignForm={editCampaignForm}
        setEditCampaignForm={setEditCampaignForm}
        handleSaveEditCampaign={handleSaveEditCampaign}
        editCampaignError={editCampaignError}
        setEditCampaignError={setEditCampaignError}
      />

      {/* Assign Users Modal */}
      {showAssignModal && assignCampaign && (
        <div style={{
          position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 200,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }} onClick={closeAssignModal}>
          <div style={{
            background: '#fff', borderRadius: 12, padding: '24px 28px', width: 420, maxWidth: '90vw',
            boxShadow: '0 20px 40px rgba(0,0,0,0.2)',
          }} onClick={e => e.stopPropagation()}>
            <h3 style={{ margin: '0 0 6px', fontSize: 16, color: '#111827' }}>Assign Users</h3>
            <p style={{ margin: '0 0 16px', fontSize: 13, color: '#6b7280' }}>
              {assignCampaign.name}
            </p>

            {assignLoading ? (
              <div style={{ color: '#9ca3af', fontSize: 13 }}>Loading...</div>
            ) : assignableUsers.length === 0 ? (
              <div style={{ color: '#9ca3af', fontSize: 13 }}>No agents or team leaders available.</div>
            ) : (
              <div style={{ maxHeight: 280, overflowY: 'auto', border: '1px solid #e5e7eb', borderRadius: 8, padding: '8px 0' }}>
                {assignableUsers.map(u => (
                  <label key={u.id} style={{
                    display: 'flex', alignItems: 'center', gap: 10,
                    padding: '8px 14px', cursor: 'pointer', fontSize: 13, color: '#374151',
                  }}>
                    <input
                      type="checkbox"
                      checked={selectedUserIds.includes(u.id)}
                      onChange={() => toggleUserSelection(u.id)}
                    />
                    <span style={{ flex: 1 }}>{u.full_name || u.email}</span>
                    <span style={{ fontSize: 11, color: '#9ca3af' }}>{u.role === 'TeamLeader' ? 'Team Leader' : u.role}</span>
                  </label>
                ))}
              </div>
            )}

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 20 }}>
              <button
                onClick={closeAssignModal}
                style={{ background: '#fff', color: '#374151', border: '1px solid #e5e7eb', borderRadius: 8, padding: '8px 16px', fontSize: 13, cursor: 'pointer' }}
              >
                Cancel
              </button>
              <button
                onClick={handleAssignUsers}
                disabled={assignLoading}
                style={{ background: '#6366f1', color: '#fff', border: 'none', borderRadius: 8, padding: '8px 16px', fontSize: 13, cursor: 'pointer' }}
              >
                {assignLoading ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

    </div>
  );
}
