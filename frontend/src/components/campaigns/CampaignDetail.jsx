import React, { useState, useEffect, useRef, useCallback } from 'react';
import { formatDateTime } from '../../utils/dateFormat';
import { VOICE_RECOMMENDATIONS } from '../../constants/voices';
import AuthAudio from '../AuthAudio';
import { useToast, useConfirm } from '../../contexts/UIContext';
import { useHideAiFeatures } from '../../hooks/useHideAiFeatures';
import { useCall } from '../../contexts/CallContext';
import { useAuth } from '../../contexts/AuthContext';
import { isValidPhone, normalizePhone, PHONE_VALIDATION_MESSAGE } from '../../utils/phone';
import { LEAD_STATUSES } from '../../constants/leadStatuses';
import { isAdmin, isAgent, isExecutive } from '../../utils/roles';
// import TwilioBrowserCallModal from './TwilioBrowserCallModal';

const T = {
  bg: '#f4f5f9', card: '#ffffff', border: '#e5e7eb',
  accent: '#6366f1', pink: '#ec4899', green: '#10b981',
  amber: '#f59e0b', red: '#ef4444', wa: '#25D366',
  text: '#111827', sub: '#374151', muted: '#9ca3af',
  font: "'DM Sans', sans-serif", mono: "'DM Mono', monospace",
};

const card = {
  background: T.card, border: `1px solid ${T.border}`,
  borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.04)',
};

const btnPrimary = {
  background: T.accent, border: 'none', color: '#fff',
  borderRadius: 8, padding: '8px 18px', cursor: 'pointer',
  fontSize: 13, fontWeight: 600, fontFamily: T.font,
};

const btnGhost = {
  background: '#fff', border: `1px solid ${T.border}`, color: T.sub,
  borderRadius: 8, padding: '6px 14px', cursor: 'pointer',
  fontSize: 12, fontWeight: 600, fontFamily: T.font,
};

function mergeProviderAccount(accounts, account) {
  const list = Array.isArray(accounts) ? [...accounts] : [];
  if (!account?.id) return list;
  return list.some(a => String(a.id) === String(account.id)) ? list : [...list, account];
}

function withDate(label, tsMs) {
  label = String(label || '');
  const d = new Date(tsMs || Date.now());
  const dd = String(d.getDate()).padStart(2, '0');
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const yyyy = d.getFullYear();
  const dateStr = `${dd}/${mm}/${yyyy}`;
  if (/\[\d{2}:\d{2}:\d{2}\]/.test(label)) {
    return label.replace(/\[(\d{2}:\d{2}:\d{2})\]/, `[${dateStr} $1]`);
  }
  return `[${dateStr}] ${label}`;
}

function linkify(text) {
  if (!text) return text;
  const parts = text.split(/(https?:\/\/[^\s]+)/g);
  return parts.map((p, i) =>
    /^https?:\/\//.test(p)
      ? <a key={i} href={p} target="_blank" rel="noreferrer"
          style={{ color: '#6366f1', textDecoration: 'underline', wordBreak: 'break-all' }}
          onClick={e => e.stopPropagation()}>{p}</a>
      : p
  );
}

async function downloadCSV({ apiFetch, url, filename, toast }) {
  try {
    const res = await apiFetch(url);
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      throw new Error(text || `Export failed (${res.status})`);
    }
    const blob = await res.blob();
    const objectUrl = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = objectUrl;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(objectUrl);
    if (toast) toast('Export downloaded');
  } catch (e) {
    if (toast) toast(`Export failed: ${e.message}`);
    throw e;
  }
}

// Mask a phone number for the auto-dial panel: keep the first 5 digits and
// replace the rest with X so agents see a list but not full numbers.
function maskPhone(phone) {
  if (!phone) return '-';
  const digits = String(phone).replace(/\D/g, '');
  if (digits.length <= 5) return digits;
  return digits.slice(0, 5) + 'X'.repeat(digits.length - 5);
}

// ── Auto Dial Panel ───────────────────────────────────────────────────────────
// Shown while browser auto-dial is active. Hides the full lead table and only
// displays the current/next call with masked phone numbers.
function AutoDialPanel({
  autoDialEnabled,
  autoDialQueue,
  autoDialActiveId,
  autoDialUninterrupted,
  onToggleUninterrupted,
  paginatedLeads,
  autoDialLeads,
  browserCallLead,
  browserCallDialing,
  onStart,
  onStop,
  campaignName,
}) {
  if (!autoDialEnabled) return null;

  const leadPool = Array.isArray(autoDialLeads) && autoDialLeads.length > 0 ? autoDialLeads : paginatedLeads;
  const queueLeads = autoDialQueue
    .map(id => leadPool.find(l => l.id === id))
    .filter(Boolean);

  const activeLead = browserCallLead
    || queueLeads.find(l => l.id === autoDialActiveId)
    || queueLeads[0];

  const activeIdx = activeLead ? queueLeads.findIndex(l => l.id === activeLead.id) : -1;
  const nextLead = queueLeads[activeIdx + 1];
  const total = queueLeads.length;
  const completed = Math.max(0, activeIdx);

  return (
    <div style={{ ...card, padding: '1.5rem', marginBottom: '1.5rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
        <div>
          <h3 style={{ margin: 0, color: T.text, fontSize: 18, fontWeight: 700 }}>▶ Auto Dial Active</h3>
          <p style={{ margin: '4px 0 0', color: T.muted, fontSize: '0.85rem' }}>{campaignName}</p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: '0.85rem', color: T.sub, fontWeight: 600 }}>
            {completed} / {total} completed
          </span>
          <button onClick={onStop} style={{ ...btnGhost, color: T.red, borderColor: 'rgba(239,68,68,0.3)' }}>
            ⏹ Stop
          </button>
        </div>
      </div>

      <label style={{
        display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer',
        fontSize: '0.85rem', color: T.sub, marginBottom: '1rem', userSelect: 'none'
      }}>
        <input
          type="checkbox"
          checked={autoDialUninterrupted}
          onChange={(e) => onToggleUninterrupted(e.target.checked)}
          style={{ width: 16, height: 16, accentColor: T.accent }}
        />
        <span>
          <strong style={{ color: T.text }}>Uninterrupted mode</strong> — skip the post-call disposition screen and automatically dial the next lead until the batch is finished.
        </span>
      </label>

      {browserCallLead || browserCallDialing ? (
        <div style={{
          background: 'rgba(99,102,241,0.06)', border: `1px solid ${T.border}`, borderRadius: 12,
          padding: '1rem', marginBottom: '1rem'
        }}>
          <div style={{ fontSize: '0.75rem', color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 6 }}>
            Now calling
          </div>
          <div style={{ fontSize: '1.1rem', fontWeight: 700, color: T.text }}>
            {activeLead?.first_name} {activeLead?.last_name}
          </div>
          <div style={{ fontFamily: T.mono, fontSize: '0.9rem', color: T.sub, marginTop: 4 }}>
            {maskPhone(activeLead?.phone)}
          </div>
        </div>
      ) : activeLead ? (
        <div style={{
          background: 'rgba(16,185,129,0.06)', border: `1px solid ${T.border}`, borderRadius: 12,
          padding: '1rem', marginBottom: '1rem'
        }}>
          <div style={{ fontSize: '0.75rem', color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 6 }}>
            Next call
          </div>
          <div style={{ fontSize: '1.1rem', fontWeight: 700, color: T.text }}>
            {activeLead.first_name} {activeLead.last_name}
          </div>
          <div style={{ fontFamily: T.mono, fontSize: '0.9rem', color: T.sub, marginTop: 4 }}>
            {maskPhone(activeLead.phone)}
          </div>
        </div>
      ) : (
        <div style={{ color: T.muted, fontSize: '0.9rem', marginBottom: '1rem' }}>
          No leads in the current view to auto-dial.
        </div>
      )}

      {nextLead && (
        <div style={{
          background: '#f8fafc', border: `1px solid ${T.border}`, borderRadius: 12,
          padding: '1rem', marginBottom: '1rem', opacity: 0.9
        }}>
          <div style={{ fontSize: '0.75rem', color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 6 }}>
            Up next
          </div>
          <div style={{ fontSize: '1rem', fontWeight: 600, color: T.text }}>
            {nextLead.first_name} {nextLead.last_name}
          </div>
          <div style={{ fontFamily: T.mono, fontSize: '0.85rem', color: T.sub, marginTop: 4 }}>
            {maskPhone(nextLead.phone)}
          </div>
        </div>
      )}

      {!browserCallLead && !browserCallDialing && activeLead && (
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <button
            onClick={() => onStart(activeLead)}
            disabled={browserCallDialing}
            style={{ ...btnPrimary, opacity: browserCallDialing ? 0.6 : 1 }}>
            {browserCallDialing ? 'Starting…' : 'Start Dialing'}
          </button>
        </div>
      )}
    </div>
  );
}

// ── WhatsApp Blast Panel ──────────────────────────────────────────────────────
function WhatsAppBlastPanel({ campaignId, apiFetch, API_URL }) {
  const [blasting, setBlasting] = useState(false);
  const [job, setJob] = useState(null);
  const [error, setError] = useState('');
  const pollRef = useRef(null);

  const stopPoll = () => { if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; } };

  const pollStatus = (jobId) => {
    stopPoll();
    pollRef.current = setInterval(async () => {
      try {
        const res = await apiFetch(`${API_URL}/wa/campaign-blast/status/${jobId}`);
        const data = await res.json();
        setJob(data);
        if (data.status !== 'running') stopPoll();
      } catch { stopPoll();  }
    }, 2000);
  };

  useEffect(() => () => stopPoll(), []);

  const handleBlast = async () => {
    setError('');
    setBlasting(true);
    setJob(null);
    try {
      const res = await apiFetch(`${API_URL}/wa/campaign-blast/${campaignId}`, { method: 'POST' });
      const data = await res.json();
      if (!res.ok) { setError(data.error || 'Blast failed'); setBlasting(false); return; }
      if (data.sent !== undefined && data.total === 0) {
        setJob({ status: 'done', total: 0, sent: 0, failed: 0, errors: [] });
        setBlasting(false);
        return;
      }
      setJob({ status: 'running', total: data.total, sent: 0, failed: 0, errors: [] });
      pollStatus(data.job_id);
    } catch { setError('Network error');  }
    setBlasting(false);
  };

  const isRunning = job?.status === 'running';
  const isDone = job?.status === 'done';
  const progress = job ? Math.round(((job.sent + job.failed) / Math.max(job.total, 1)) * 100) : 0;

  return (
    <div style={{ marginBottom: '1rem' }}>
      {error && (
        <div style={{ background: '#fee2e2', border: `1px solid #fca5a5`, color: T.red, borderRadius: 8, padding: '10px 14px', marginBottom: 10, fontSize: '0.85rem' }}>
          ⚠️ {error}
        </div>
      )}
      {!isRunning && !isDone && (
        <button
          style={{ background: `linear-gradient(135deg, ${T.wa}, #128C7E)`, border: 'none', color: '#fff', fontSize: '0.85rem', padding: '8px 18px', borderRadius: 8, cursor: 'pointer', fontWeight: 600, fontFamily: T.font }}
          disabled={blasting}
          onClick={handleBlast}>
          {blasting ? 'Starting...' : '💬 Send to New Leads'}
        </button>
      )}
      {(isRunning || isDone) && (
        <div style={{ ...card, padding: '12px 16px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8, fontSize: '0.85rem', color: T.sub }}>
            <span>{isRunning ? '⏳ Sending...' : '✅ Blast complete'}</span>
            <span style={{ color: T.muted }}>{job.sent} sent · {job.failed} failed · {job.total} total</span>
          </div>
          <div style={{ background: T.border, borderRadius: 4, height: 6, overflow: 'hidden' }}>
            <div style={{ width: `${progress}%`, height: '100%', background: `linear-gradient(90deg, ${T.wa}, #128C7E)`, transition: 'width 0.4s' }} />
          </div>
          {isDone && job.failed > 0 && (
            <div style={{ marginTop: 8, fontSize: '0.75rem', color: T.amber }}>
              {job.errors?.slice(0, 3).map((e, i) => <div key={i}>{e}</div>)}
              {job.errors?.length > 3 && <div>…and {job.errors.length - 3} more</div>}
            </div>
          )}
          {isDone && (
            <button onClick={() => { setJob(null); setError(''); }}
              style={{ marginTop: 8, background: '#fff', border: `1px solid ${T.border}`, color: T.muted, borderRadius: 6, padding: '4px 10px', cursor: 'pointer', fontSize: '0.75rem', fontFamily: T.font }}>
              Send Again
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function AuthAudioPlayer({ src, style }) {
  const [blobUrl, setBlobUrl] = React.useState(null);
  const [err, setErr] = React.useState(false);
  const audioRef = React.useRef(null);
  const seekedRef = React.useRef(false);

  React.useEffect(() => {
    if (!src) return;
    let objectUrl;
    fetch(src, { credentials: 'include' })
      .then(r => { if (!r.ok) throw new Error(r.status); return r.blob(); })
      .then(blob => { objectUrl = URL.createObjectURL(blob); setBlobUrl(objectUrl); })
      .catch(() => setErr(true));
    return () => { if (objectUrl) URL.revokeObjectURL(objectUrl); };
  }, [src]);

  // WebM files from MediaRecorder omit duration metadata; seeking to a huge
  // timestamp forces the browser to scan the file and report the real duration.
  const handleLoadedMetadata = () => {
    const a = audioRef.current;
    if (!a || seekedRef.current) return;
    if (!isFinite(a.duration) || a.duration === 0) {
      seekedRef.current = true;
      a.currentTime = 1e101;
    }
  };
  const handleSeeked = () => {
    const a = audioRef.current;
    if (!a || !seekedRef.current) return;
    seekedRef.current = false;
    a.currentTime = 0;
  };

  if (err) return <span style={{color:'#f87171',fontSize:'0.75rem'}}>Unavailable</span>;
  if (!blobUrl) return <span style={{color:'#64748b',fontSize:'0.75rem'}}>Loading…</span>;
  return (
    <audio ref={audioRef} controls style={style} src={blobUrl}
      onLoadedMetadata={handleLoadedMetadata} onSeeked={handleSeeked} />
  );
}

export default function CampaignDetail({
  selectedCampaign, setSelectedCampaign,
  campaignLeads, callLog, detailTab, setDetailTab,
  handleBack, fetchCampaignLeads, fetchCallLog, fetchCampaigns,
  statusBadge, getProductName, getCampaignStats,
  campVoice, setCampVoice, handleSaveCampVoice, handleResetCampVoice, campVoiceSaveStatus,
  INDIAN_VOICES, INDIAN_LANGUAGES,
  liveEvents, setLiveEvents,
  handleLeadStatusChange, handleEditLead, handleRemoveLead, handleDeleteLead,
  campaignLeadsTotal,
  handleViewTranscripts,
  onCampaignDial, onCampaignWebCall,
  dialingId, webCallActive,
  setSelectedLeadIds, setShowAddLeadsModal, setShowCsvImportModal, setCsvFile,
  apiFetch, API_URL, orgTimezone,
  handleEditCampaign,
  executives,
  agents = [],
  detailExecutiveFilter, setDetailExecutiveFilter
}) {
  const stats = getCampaignStats(selectedCampaign);
  const toast = useToast();
  const confirm = useConfirm();
  const { currentUser, hasPermission } = useAuth();
  const hideAiFeatures = useHideAiFeatures();
  const canShowAgentFilter = isAdmin(currentUser?.role);
  const canCreateLead = hasPermission('crm.create');
  const canEditLead = hasPermission('crm.edit');
  const canDeleteLead = hasPermission('crm.delete');
  const canImportLeads = hasPermission('crm.import');
  const canExportLeads = hasPermission('crm.export');
  const canAssignLeads = hasPermission('crm.assign');
  const canEditCampaign = hasPermission('campaigns.edit');
  const canDial = !hideAiFeatures && hasPermission('calls.dial');
  const canDialAll = !hideAiFeatures && hasPermission('calls.dial_all');
  const canBrowserCall = hasPermission('calls.browser_call');
  const canAutoDial = hasPermission('calls.auto_dial');
  const canMakeCalls = canDial || canDialAll || canBrowserCall || canAutoDial;
  const canScheduleCalls = hasPermission('calls.schedule');
  const canViewTranscripts = hasPermission('calls.transcripts');
  const canViewRecordings = hasPermission('calls.recordings');
  const canViewReports = hasPermission('reports.view');
  const canSaveVoiceSettings = hasPermission('voice_settings.save');
  const currentExecutiveLabel = currentUser?.full_name || currentUser?.name || currentUser?.email || 'You';
  const executiveNameForLead = (lead) => {
    const assigned = executives.find(e => String(e.id) === String(lead.executive_id));
    if (assigned) return assigned.name || assigned.full_name || assigned.email;
    if (isExecutive(currentUser?.role)) return currentExecutiveLabel;
    return '— Unassigned —';
  };
  // Only Executives are restricted from changing the per-machine browser call account;
  // Admins/Agents can always change it, and Executives can if granted the permission.
  const canChangeBrowserCallAccount = !isExecutive(currentUser?.role) || hasPermission('calls.browser_call_account');
  const mustSelectBrowserCallAccount = isAgent(currentUser?.role) || isExecutive(currentUser?.role);
  const { triggerBrowserCall, browserCallLead, browserCallDialing, refreshScheduledCalls, clearDismissedScheduledCall } = useCall();
  const [callInsights, setCallInsights] = useState(null);
  const [callReviews, setCallReviews] = useState([]);
  const [callOutcomeStats, setCallOutcomeStats] = useState({ total: 0, connected: 0, completed: 0, unanswered: 0, busy: 0, failed: 0 });
  const [insightsLoading, setInsightsLoading] = useState(false);
  const [insightsError, setInsightsError] = useState('');
  const [billingUsage, setBillingUsage] = useState(null);
  const [retries, setRetries] = useState([]);
  const [retriesLoading, setRetriesLoading] = useState(false);
  const [scheduleLead, setScheduleLead] = useState(null);
  const [scheduleAt, setScheduleAt] = useState('');
  const [scheduleNotes, setScheduleNotes] = useState('');
  const [scheduleMode, setScheduleMode] = useState('manual');
  const [scheduleEditingCallId, setScheduleEditingCallId] = useState(0);
  const [scheduleActionLeadId, setScheduleActionLeadId] = useState(null);
  const [scheduleSaving, setScheduleSaving] = useState(false);
  const [scheduleStatus, setScheduleStatus] = useState({ kind: '', text: '' });
  const [scheduleError, setScheduleError] = useState('');
  const [qaStatus, setQaStatus] = useState(null);
  const [leadSearch, setLeadSearch] = useState('');
  // ── Bulk executive assignment state ─────────────────────────────────────────
  const [bulkSelectedIds, setBulkSelectedIds] = useState(new Set());
  const [bulkSelectedLeads, setBulkSelectedLeads] = useState([]);
  const [bulkSelectAll, setBulkSelectAll] = useState(false);
  const [bulkAssigning, setBulkAssigning] = useState(false);
  const [showBulkAssignMenu, setShowBulkAssignMenu] = useState(false);
  const [showBulkSelectMenu, setShowBulkSelectMenu] = useState(false);
  const [bulkSelectLimit, setBulkSelectLimit] = useState('');
  const [bulkSelectionLoading, setBulkSelectionLoading] = useState(false);
  const [execFilter, setExecFilter] = useState([]);
  const [showExecFilter, setShowExecFilter] = useState(false);
  const [execSearch, setExecSearch] = useState('');
  const [showDetailExecFilter, setShowDetailExecFilter] = useState(false);
  const [detailExecSearch, setDetailExecSearch] = useState('');
  const [scheduleFrom, setScheduleFrom] = useState('');
  const [scheduleTo, setScheduleTo] = useState('');
  const currentCampaignId = Number(
    selectedCampaign?.id || selectedCampaign?.campaign_id || selectedCampaign?.campaignId || 0
  );

  // ── Lead-table pagination ───────────────────────────────────────────────────
  const PAGE_SIZE = 100;
  const [currentPage, setCurrentPage] = useState(1);
  const [jumpPage, setJumpPage] = useState('');

  // ── Auto-dialer state (Browser Call only) ───────────────────────────────────
  const [autoDialEnabled, setAutoDialEnabled] = useState(false);
  const [autoDialQueue, setAutoDialQueue] = useState([]);
  const [autoDialActiveId, setAutoDialActiveId] = useState(null);
  const [autoDialSelectedOnly, setAutoDialSelectedOnly] = useState(false);
  // Uninterrupted mode: skip the post-call disposition modal and auto-advance
  // to the next lead until the queue is exhausted.
  const [autoDialUninterrupted, setAutoDialUninterrupted] = useState(false);

  // ── Disposition modal state (post-call before next auto-dial) ───────────────
  const [showDispositionModal, setShowDispositionModal] = useState(false);
  const [dispositionLead, setDispositionLead] = useState(null);
  const [dispositionStatus, setDispositionStatus] = useState('');
  const [dispositionRemarks, setDispositionRemarks] = useState('');
  const [dispositionFollowUpAt, setDispositionFollowUpAt] = useState('');
  const [dispositionSaving, setDispositionSaving] = useState(false);
  const [dispositionNextLead, setDispositionNextLead] = useState(null);

  // Browser-call account for this machine. When a specific account is selected
  // it is also persisted as the campaign default so AI auto-dial and external
  // API calls route through the same provider account (e.g. Tata Tele).
  const [browserAccountId, setBrowserAccountId] = useState('');
  const browserAccountKey = useCallback((id) => `callified_browser_account_campaign_${id}`, []);
  const [orgExotelAccounts, setOrgExotelAccounts] = useState([]);
  const [selectedExotelAccountId, setSelectedExotelAccountId] = useState('');
  // If the user lacks permission to change the per-machine browser call account,
  // force the fixed campaign/lead assignment account and ignore any localStorage override.
  const effectiveBrowserAccountId = canChangeBrowserCallAccount
    ? (mustSelectBrowserCallAccount ? browserAccountId : (browserAccountId || selectedExotelAccountId))
    : selectedExotelAccountId;
  const hasBrowserCallAccount = String(effectiveBrowserAccountId || '').trim() !== '';
  const effectiveBrowserAccount = orgExotelAccounts.find(a => String(a.id) === String(effectiveBrowserAccountId));

  const openScheduleModal = useCallback((lead, editing = false) => {
    setScheduleEditingCallId(editing ? Number(lead?.scheduled_call_id || 0) : 0);
    setScheduleLead(lead);
    setScheduleStatus({ kind: '', text: '' });
    setScheduleError('');
  }, []);

  const closeScheduleModal = useCallback(() => {
    setScheduleLead(null);
    setScheduleEditingCallId(0);
    setScheduleStatus({ kind: '', text: '' });
    setScheduleError('');
  }, []);

  useEffect(() => {
    const onDocClick = () => setScheduleActionLeadId(null);
    if (scheduleActionLeadId == null) return undefined;
    document.addEventListener('click', onDocClick);
    return () => document.removeEventListener('click', onDocClick);
  }, [scheduleActionLeadId]);

  useEffect(() => {
    setExecFilter([]);
    setExecSearch('');
    setShowExecFilter(false);
    setDetailExecutiveFilter([]);
    setDetailExecSearch('');
    setShowDetailExecFilter(false);
    setAutoDialEnabled(false);
    setAutoDialQueue([]);
    setAutoDialActiveId(null);
    setAutoDialSelectedOnly(false);
    setAutoDialUninterrupted(false);
    setShowDispositionModal(false);
    setDispositionLead(null);
    setDispositionNextLead(null);
    setScheduleFrom('');
    setScheduleTo('');
    setCurrentPage(1);
    setBulkSelectedIds(new Set());
    setBulkSelectedLeads([]);
    setBulkSelectAll(false);
    setShowBulkSelectMenu(false);
    setBulkSelectLimit('');
  }, [selectedCampaign?.id, setDetailExecutiveFilter]);

  useEffect(() => {
    if (detailTab === 'calllog' && !canViewTranscripts && !canViewRecordings) setDetailTab('leads');
    if ((detailTab === 'insights' || detailTab === 'retries') && (!canViewReports || hideAiFeatures)) setDetailTab('leads');
  }, [detailTab, canViewTranscripts, canViewRecordings, canViewReports, hideAiFeatures, setDetailTab]);

  // Server-side pagination: fetch the current page with active filters.
  const loadCampaignLeads = useCallback(() => {
    if (!selectedCampaign?.id) return;
    fetchCampaignLeads(selectedCampaign.id, {
      page: currentPage,
      limit: PAGE_SIZE,
      search: leadSearch.trim(),
      executiveIds: execFilter,
      scheduledFrom: scheduleFrom,
      scheduledTo: scheduleTo,
    });
  }, [selectedCampaign?.id, currentPage, leadSearch, execFilter, scheduleFrom, scheduleTo, fetchCampaignLeads]);

  useEffect(() => {
    loadCampaignLeads();
  }, [loadCampaignLeads]);

  // Reset to page 1 whenever filters/search change so the user doesn't land on
  // an empty page after narrowing the list.
  useEffect(() => {
    setCurrentPage(1);
  }, [leadSearch, execFilter, scheduleFrom, scheduleTo]);

  // Clear bulk selection when filters, search, or page changes so selections don't
  // span shifting result sets.
  useEffect(() => {
    setBulkSelectedIds(new Set());
    setBulkSelectedLeads([]);
    setBulkSelectAll(false);
    setShowBulkAssignMenu(false);
    setShowBulkSelectMenu(false);
  }, [leadSearch, execFilter, scheduleFrom, scheduleTo, currentPage]);

  const totalPages = Math.ceil(campaignLeadsTotal / PAGE_SIZE);
  const safePage = Math.max(1, Math.min(currentPage, totalPages || 1));

  const handleJump = () => {
    const n = parseInt(jumpPage, 10);
    if (!isNaN(n)) {
      setCurrentPage(Math.max(1, Math.min(totalPages, n)));
    }
    setJumpPage('');
  };

  // ── Bulk executive assignment helpers ─────────────────────────────────────────
  const toggleBulkSelection = (leadId) => {
    setBulkSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(leadId)) next.delete(leadId);
      else next.add(leadId);
      setBulkSelectedLeads(current => {
        if (prev.has(leadId)) return current.filter(l => l.id !== leadId);
        const lead = paginatedLeads.find(l => l.id === leadId);
        if (!lead || current.some(l => l.id === leadId)) return current;
        return [...current, lead];
      });
      setBulkSelectAll(false);
      return next;
    });
  };

  const selectAllVisible = (checked) => {
    if (checked) {
      setBulkSelectedIds(new Set(paginatedLeads.map(l => l.id)));
      setBulkSelectedLeads(paginatedLeads);
    } else {
      setBulkSelectedIds(new Set());
      setBulkSelectedLeads([]);
      setBulkSelectAll(false);
    }
    setShowBulkSelectMenu(false);
  };

  const fetchBulkLeadSelection = async (requestedLimit) => {
    if (!currentCampaignId || bulkSelectionLoading) return;
    const targetTotal = requestedLimit === 'all'
      ? campaignLeadsTotal
      : Math.max(0, Math.min(parseInt(requestedLimit, 10) || 0, campaignLeadsTotal));
    if (targetTotal <= 0) {
      toast('No leads to select');
      return;
    }
    setBulkSelectionLoading(true);
    try {
      const batchSize = 500;
      const selected = [];
      let page = 1;
      while (selected.length < targetTotal) {
        const params = new URLSearchParams();
        params.set('page', String(page));
        params.set('limit', String(Math.min(batchSize, targetTotal - selected.length)));
        if (leadSearch.trim()) params.set('search', leadSearch.trim());
        if (execFilter?.length) params.set('executive_ids', execFilter.join(','));
        if (scheduleFrom) params.set('scheduled_from', scheduleFrom);
        if (scheduleTo) params.set('scheduled_to', scheduleTo);
        const res = await apiFetch(`${API_URL}/campaigns/${currentCampaignId}/leads?${params.toString()}`);
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data.error || `Failed to select leads (${res.status})`);
        const rows = Array.isArray(data.leads) ? data.leads : [];
        if (rows.length === 0) break;
        selected.push(...rows);
        if (selected.length >= (data.total || targetTotal)) break;
        page += 1;
      }
      const finalRows = selected.slice(0, targetTotal);
      setBulkSelectedLeads(finalRows);
      setBulkSelectedIds(new Set(finalRows.map(l => l.id)));
      setBulkSelectAll(requestedLimit === 'all' || finalRows.length >= campaignLeadsTotal);
      setShowBulkSelectMenu(false);
      toast(`${finalRows.length} lead(s) selected`);
    } catch (err) {
      toast(err.message || 'Failed to select leads');
    } finally {
      setBulkSelectionLoading(false);
    }
  };

  const selectAllCampaign = () => {
    fetchBulkLeadSelection('all');
  };

  const clearBulkSelection = () => {
    setBulkSelectedIds(new Set());
    setBulkSelectedLeads([]);
    setBulkSelectAll(false);
    setShowBulkAssignMenu(false);
    setShowBulkSelectMenu(false);
  };

  const handleBulkAssignExecutive = async (executiveValue) => {
    if (!currentCampaignId || (!bulkSelectAll && bulkSelectedIds.size === 0) || !executiveValue) return;
    const isUnassign = executiveValue === 'clear' || executiveValue === 'remove';
    const execId = isUnassign ? 0 : parseInt(executiveValue, 10);
    if (!isUnassign && (!execId || isNaN(execId))) {
      toast('Please select an executive');
      return;
    }
    setBulkAssigning(true);
    try {
      const payload = bulkSelectAll
        ? { all: true, executive_id: execId, search: leadSearch.trim(), scheduled_from: scheduleFrom, scheduled_to: scheduleTo }
        : { lead_ids: Array.from(bulkSelectedIds), executive_id: execId };
      const res = await apiFetch(`${API_URL}/campaigns/${currentCampaignId}/leads/executive`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        throw new Error(data.error || `Failed to assign executive (${res.status})`);
      }
      const affected = bulkSelectAll ? campaignLeadsTotal : (typeof data.updated === 'number' ? data.updated : bulkSelectedIds.size);
      const leadLabel = affected === 1 ? 'lead' : 'leads';
      toast(isUnassign ? `Executive unassigned from ${affected} ${leadLabel}` : `Executive assigned to ${affected} ${leadLabel}`);
      clearBulkSelection();
      fetchCampaignLeads(currentCampaignId);
    } catch (err) {
      toast(err.message || 'Failed to assign executive');
    } finally {
      setBulkAssigning(false);
    }
  };

  useEffect(() => {
    if (currentPage > totalPages && totalPages > 0) setCurrentPage(totalPages);
  }, [currentPage, totalPages]);

  const paginatedLeads = campaignLeads;
  const selectedAutoDialLeads = bulkSelectedLeads.length > 0
    ? bulkSelectedLeads
    : paginatedLeads.filter(l => bulkSelectedIds.has(l.id));
  const autoDialButtonCount = !autoDialEnabled && selectedAutoDialLeads.length > 0
    ? selectedAutoDialLeads.length
    : 0;

  // Keep the auto-dial queue in sync with the current page of leads.
  useEffect(() => {
    if (!autoDialEnabled) return;
    const sourceLeads = autoDialSelectedOnly
      ? (bulkSelectedLeads.length > 0 ? bulkSelectedLeads : paginatedLeads.filter(l => bulkSelectedIds.has(l.id)))
      : paginatedLeads;
    const ids = sourceLeads.map(l => l.id);
    setAutoDialQueue(prev => {
      if (autoDialActiveId && ids.includes(autoDialActiveId)) {
        const idx = ids.indexOf(autoDialActiveId);
        return [autoDialActiveId, ...ids.slice(idx + 1)];
      }
      return ids;
    });
  }, [paginatedLeads, bulkSelectedIds, bulkSelectedLeads, autoDialEnabled, autoDialActiveId, autoDialSelectedOnly]);

  const [editingNote, setEditingNote] = useState(null);
  const [generatedNote, setGeneratedNote] = useState(null);
  const [noteSaving, setNoteSaving] = useState(false);
  const [noteGenerating, setNoteGenerating] = useState(false);

  // Quick-note modal state (moved here from CampaignsPage so we can refresh
  // campaign leads immediately after saving and show the note label at once).
  const [noteModalLead, setNoteModalLead] = useState(null);
  const [noteModalText, setNoteModalText] = useState('');
  const [noteModalSaving, setNoteModalSaving] = useState(false);

  const handleGenerateNote = async (lead) => {
    setNoteGenerating(true);
    setGeneratedNote(null);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/generate-followup-note`, { method: 'POST' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { toast(data.error || 'Could not generate note'); return; }
      setGeneratedNote({ leadId: lead.id, text: data.note || '', recordingUrl: data.recording_url || '', recordingFilename: data.recording_filename || '' });
      setEditingNote(null);
    } catch (e) {
      toast('Failed to generate note: ' + (e?.message || 'network error'));
    } finally {
      setNoteGenerating(false);
    }
  };

  const handleSaveInlineNote = async (lead) => {
    if (!editingNote) return;
    const trimmed = editingNote.text.trim();
    setNoteSaving(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/${lead.id}/notes`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: trimmed }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        toast(data.error || `Failed to save (HTTP ${res.status})`);
        return;
      }
      lead.follow_up_note = trimmed;
      setEditingNote(null);
      setGeneratedNote(null);
      fetchCampaignLeads(selectedCampaign.id);
    } catch (e) {
      toast('Failed to save note: ' + (e?.message || 'network error'));
    } finally {
      setNoteSaving(false);
    }
  };

  const openNoteModal = (lead) => {
    setNoteModalLead(lead);
    setNoteModalText(lead.follow_up_note || '');
  };

  const handleSaveNoteModal = async () => {
    if (!noteModalLead) return;
    const trimmed = noteModalText.trim();
    if (!trimmed) { toast('Note cannot be empty'); return; }
    setNoteModalSaving(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/${noteModalLead.id}/notes`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ note: trimmed }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        toast(data.error || data.detail || `Failed to save note (HTTP ${res.status})`);
        return;
      }
      setNoteModalLead(null);
      setNoteModalText('');
      fetchCampaignLeads(selectedCampaign.id);
    } catch (e) {
      toast('Failed to save note: ' + (e?.message || 'network error'));
    } finally {
      setNoteModalSaving(false);
    }
  };

  const [waSendingId, setWaSendingId] = useState(null);
  const [waSendStatus, setWaSendStatus] = useState({}); // lead.id → 'sent' | 'error'

  const handleSendWA = async (lead) => {
    setWaSendingId(lead.id);
    setWaSendStatus(s => ({ ...s, [lead.id]: null }));
    try {
      const res = await apiFetch(`${API_URL}/wa/campaign-blast/${selectedCampaign.id}/send-one`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lead_id: lead.id }),
      });
      setWaSendStatus(s => ({ ...s, [lead.id]: res.ok ? 'sent' : 'error' }));
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        toast(data.error || `Send failed (HTTP ${res.status})`);
      }
    } catch {
      setWaSendStatus(s => ({ ...s, [lead.id]: 'error' }));
      toast('Network error — could not reach server');
    }
    setWaSendingId(null);
  };

  const [qaName, setQaName] = useState('');
  const [qaPhone, setQaPhone] = useState('');
  const [qaNameErr, setQaNameErr] = useState('');
  const [qaPhoneErr, setQaPhoneErr] = useState('');
  const [qaApiErr, setQaApiErr] = useState('');

  const [dndBlockedLeadIds, setDndBlockedLeadIds] = useState(() => new Set());
  const requireSelectedDialAccount = useCallback(() => {
    const selected = String(effectiveBrowserAccountId || '').trim();
    if (selected) return true;
    toast('Select a browser call account before calling');
    return false;
  }, [effectiveBrowserAccountId, toast]);

  const handleDialClick = async (lead) => {
    if (!requireSelectedDialAccount()) return;
    onCampaignDial(lead, selectedCampaign.id, browserAccountId);
    try {
      const res = await apiFetch(`${API_URL}/dnd/check/${encodeURIComponent(lead.phone || '')}`);
      if (!res.ok) return;
      const data = await res.json();
      if (!data.is_dnd) return;
      setDndBlockedLeadIds(prev => {
        const next = new Set(prev);
        next.add(lead.id);
        return next;
      });
      setTimeout(() => {
        setDndBlockedLeadIds(prev => {
          if (!prev.has(lead.id)) return prev;
          const next = new Set(prev);
          next.delete(lead.id);
          return next;
        });
      }, 2000);
    } catch { /* network/permission — silently skip badge */  }
  };

  const handleHumanCallDial = async () => {
    if (!humanCallLead || !humanCallPhone.trim()) return;
    localStorage.setItem('humanCallAgentPhone', humanCallPhone.trim());
    setHumanCallStatus('dialing');
    setHumanCallError('');
    try {
      const res = await apiFetch(
        `${API_URL}/campaigns/${selectedCampaign.id}/human-call/${humanCallLead.id}`,
        { method: 'POST', body: JSON.stringify({ agent_phone: humanCallPhone.trim() }) }
      );
      if (!res.ok) {
        const d = await res.json().catch(() => ({}));
        throw new Error(d.error || `HTTP ${res.status}`);
      }
      setHumanCallStatus('done');
      setTimeout(() => { setHumanCallLead(null); setHumanCallStatus('idle'); }, 2000);
    } catch (e) {
      setHumanCallError(e.message || 'Dial failed');
      setHumanCallStatus('error');
    }
  };

  // Refs keep auto-dial state fresh inside the ended-callback without
  // recreating the callback on every render.
  const autoDialEnabledRef = useRef(autoDialEnabled);
  const autoDialActiveIdRef = useRef(autoDialActiveId);
  const autoDialQueueRef = useRef(autoDialQueue);
  const autoDialUninterruptedRef = useRef(autoDialUninterrupted);
  const campaignLeadsRef = useRef(campaignLeads);
  const paginatedLeadsRef = useRef(paginatedLeads);
  const bulkSelectedLeadsRef = useRef(bulkSelectedLeads);
  useEffect(() => { autoDialEnabledRef.current = autoDialEnabled; }, [autoDialEnabled]);
  useEffect(() => { autoDialActiveIdRef.current = autoDialActiveId; }, [autoDialActiveId]);
  useEffect(() => { autoDialQueueRef.current = autoDialQueue; }, [autoDialQueue]);
  useEffect(() => { autoDialUninterruptedRef.current = autoDialUninterrupted; }, [autoDialUninterrupted]);
  useEffect(() => { campaignLeadsRef.current = campaignLeads; }, [campaignLeads]);
  useEffect(() => { paginatedLeadsRef.current = paginatedLeads; }, [paginatedLeads]);
  useEffect(() => { bulkSelectedLeadsRef.current = bulkSelectedLeads; }, [bulkSelectedLeads]);

  const advanceAutoDial = useCallback((status, errorMsg) => {
    const terminalError = status === 'error';
    if (terminalError && (!autoDialEnabledRef.current || !autoDialActiveIdRef.current || !autoDialUninterruptedRef.current)) {
      toast('Auto dial stopped: browser call failed');
      setAutoDialEnabled(false);
      setAutoDialActiveId(null);
      setAutoDialQueue([]);
      setAutoDialSelectedOnly(false);
      return;
    }
    if (!autoDialEnabledRef.current || !autoDialActiveIdRef.current) return;

    // Find the lead that just finished so the agent can disposition it.
    const finishedId = autoDialActiveIdRef.current;
    const finishedLead = campaignLeadsRef.current.find(l => l.id === finishedId) || paginatedLeadsRef.current.find(l => l.id === finishedId) || bulkSelectedLeadsRef.current.find(l => l.id === finishedId);

    // Determine the next lead in the queue (if any).
    const idx = autoDialQueueRef.current.indexOf(finishedId);
    const nextIdx = idx >= 0 ? idx + 1 : autoDialQueueRef.current.length;
    const nextId = autoDialQueueRef.current[nextIdx];
    const nextLead = nextId ? (campaignLeadsRef.current.find(l => l.id === nextId) || paginatedLeadsRef.current.find(l => l.id === nextId) || bulkSelectedLeadsRef.current.find(l => l.id === nextId)) : null;

    if (!finishedLead) {
      toast('Auto dial stopped: lead not found');
      setAutoDialEnabled(false);
      setAutoDialActiveId(null);
      setAutoDialQueue([]);
      setAutoDialSelectedOnly(false);
      return;
    }

    // Uninterrupted mode: skip the disposition modal and dial the next lead
    // automatically. When the queue is exhausted, stop cleanly.
    if (autoDialUninterruptedRef.current) {
      if (nextLead) {
        setTimeout(async () => {
          for (let i = nextIdx; i < autoDialQueueRef.current.length; i += 1) {
            const id = autoDialQueueRef.current[i];
            const lead = campaignLeadsRef.current.find(l => l.id === id) || paginatedLeadsRef.current.find(l => l.id === id) || bulkSelectedLeadsRef.current.find(l => l.id === id);
            if (!lead) continue;
            const started = await triggerBrowserCall(lead, selectedCampaign.id, advanceAutoDial, effectiveBrowserAccountId);
            if (started) {
              setAutoDialActiveId(lead.id);
              return;
            }
          }
          toast('Auto dial complete');
          setAutoDialEnabled(false);
          setAutoDialActiveId(null);
          setAutoDialQueue([]);
          setAutoDialSelectedOnly(false);
        }, terminalError ? 800 : 400);
        return;
      }
      toast('Auto dial complete');
      setAutoDialEnabled(false);
      setAutoDialActiveId(null);
      setAutoDialQueue([]);
      setAutoDialSelectedOnly(false);
      return;
    }

    if (!nextId) {
      // Last lead in the queue: still show disposition, then mark complete after save.
      setDispositionNextLead(null);
    } else {
      setDispositionNextLead(nextLead);
    }

    // Pause auto-dial and show the disposition modal.
    setDispositionLead(finishedLead);
    setDispositionStatus(finishedLead.status || 'Connected');
    setDispositionRemarks(finishedLead.follow_up_note || '');
    setDispositionFollowUpAt(finishedLead.follow_up_at ? finishedLead.follow_up_at.slice(0, 16) : '');
    setShowDispositionModal(true);
  }, [toast, triggerBrowserCall, selectedCampaign.id, effectiveBrowserAccountId]);

  const startNextAutoDialLead = useCallback(async (startIdx) => {
    for (let i = startIdx; i < autoDialQueueRef.current.length; i += 1) {
      const id = autoDialQueueRef.current[i];
      const lead = campaignLeadsRef.current.find(l => l.id === id) || paginatedLeadsRef.current.find(l => l.id === id) || bulkSelectedLeadsRef.current.find(l => l.id === id);
      if (!lead) continue;
      const started = await triggerBrowserCall(lead, selectedCampaign.id, advanceAutoDial, effectiveBrowserAccountId);
      if (started) {
        setAutoDialActiveId(lead.id);
        return true;
      }
    }
    toast('Auto dial complete');
    setAutoDialEnabled(false);
    setAutoDialActiveId(null);
    setAutoDialQueue([]);
    setAutoDialSelectedOnly(false);
    return false;
  }, [advanceAutoDial, effectiveBrowserAccountId, selectedCampaign.id, toast, triggerBrowserCall]);

  const saveDispositionAndAdvance = useCallback(async (stopAfterSave) => {
    if (!dispositionLead) return;
    if (!dispositionStatus.trim()) {
      toast('Please select a call status');
      return;
    }
    setDispositionSaving(true);
    try {
      const res = await apiFetch(`${API_URL}/leads/${dispositionLead.id}/disposition`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          campaign_id: selectedCampaign.id,
          status: dispositionStatus.trim(),
          note: dispositionRemarks.trim(),
          follow_up_at: dispositionFollowUpAt || ''
        })
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || `Save failed (HTTP ${res.status})`);
      }
      toast('Disposition saved');
      fetchCampaignLeads(selectedCampaign.id);
    } catch (e) {
      toast(`Failed to save disposition: ${e.message}`);
      setDispositionSaving(false);
      return;
    }
    setDispositionSaving(false);
    setShowDispositionModal(false);
    setDispositionLead(null);

    if (stopAfterSave || !dispositionNextLead) {
      setAutoDialEnabled(false);
      setAutoDialActiveId(null);
      setAutoDialQueue([]);
      setAutoDialSelectedOnly(false);
      if (dispositionNextLead) {
        toast('Auto dial stopped');
      } else {
        toast('Auto dial complete');
      }
      return;
    }

    // Advance to the next lead only after the browser can place the call.
    setTimeout(async () => {
      const nextIdx = autoDialQueueRef.current.indexOf(dispositionNextLead.id);
      await startNextAutoDialLead(nextIdx >= 0 ? nextIdx : 0);
    }, 400);
  }, [dispositionLead, dispositionStatus, dispositionRemarks, dispositionFollowUpAt, dispositionNextLead, apiFetch, API_URL, selectedCampaign.id, fetchCampaignLeads, startNextAutoDialLead, toast]);

  const startBrowserCallWithAutoDial = async (lead) => {
    if (!requireSelectedDialAccount()) return;
    const started = await triggerBrowserCall(lead, selectedCampaign.id, autoDialEnabled ? advanceAutoDial : undefined, effectiveBrowserAccountId);
    if (started && autoDialEnabled) {
      setAutoDialActiveId(lead.id);
      const queueSource = autoDialSelectedOnly
        ? (bulkSelectedLeads.length > 0 ? bulkSelectedLeads : paginatedLeads.filter(l => bulkSelectedIds.has(l.id)))
        : paginatedLeads;
      const ids = queueSource.map(l => l.id);
      const idx = ids.indexOf(lead.id);
      if (idx >= 0) {
        setAutoDialQueue([lead.id, ...ids.slice(idx + 1)]);
      } else {
        setAutoDialQueue([lead.id]);
      }
    }
  };

  const [confirmDialAction, setConfirmDialAction] = useState(null); // { type: 'new'|'all'|'redial', label, count }

  // Refresh call log after sim web call ends — poll multiple times to catch
  // transcripts that are written asynchronously (Deepgram, WAV mux, DB write).
  const prevWebCallActiveRef = React.useRef(webCallActive);
  useEffect(() => {
    if (prevWebCallActiveRef.current !== null && webCallActive === null) {
      const id = selectedCampaign.id;
      // t=4s catches fast calls; t=9s and t=16s catch slow Deepgram/WAV paths
      [4000, 9000, 16000].forEach(delay =>
        setTimeout(() => fetchCallLog(id), delay)
      );
    }
    prevWebCallActiveRef.current = webCallActive;
  }, [webCallActive]);

  // Pre-fill date/time to current time every time the modal opens for a lead.
  useEffect(() => {
    if (!scheduleLead) return;
    if (scheduleEditingCallId) {
      const d = scheduleLead.next_scheduled_at ? new Date(scheduleLead.next_scheduled_at) : new Date();
      const p = n => String(n).padStart(2, '0');
      setScheduleAt(`${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`);
      setScheduleNotes(scheduleLead.scheduled_call_notes || '');
      setScheduleError('');
      setScheduleMode(scheduleLead.scheduled_call_mode || 'manual');
      return;
    }
    const d = new Date();
    const p = n => String(n).padStart(2, '0');
    setScheduleAt(`${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`);
    setScheduleNotes('');
    setScheduleError('');
    setScheduleMode('manual');
  }, [scheduleLead, scheduleEditingCallId]);

  const handleScheduleCall = async () => {
    if (!scheduleLead || !scheduleAt) return;
    // Reject times in the past or less than 1 minute from now
    if (new Date(scheduleAt) <= new Date(Date.now() - 60 * 1000)) {
      setScheduleError('Please select a future date and time.');
      return;
    }
    setScheduleSaving(true);
    setScheduleError('');
    try {
      // Convert browser-local datetime → UTC ISO so the backend scheduler
      // (which compares against UTC NOW()) fires at exactly the right moment.
      const utcTime = new Date(scheduleAt).toISOString();
      const res = await apiFetch(`${API_URL}/scheduled-calls`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ lead_id: scheduleLead.id, campaign_id: selectedCampaign.id, scheduled_at: utcTime, notes: scheduleNotes })
      });
      if (res.ok) {
        setScheduleLead(null);
        setScheduleAt('');
        setScheduleNotes('');
        fetchCampaignLeads(selectedCampaign.id);
      } else {
        const d = await res.json().catch(() => ({}));
        setScheduleError(d.detail || d.error || `Error ${res.status}`);
      }
    } catch(e) { setScheduleError('Network error — please try again.'); }
    setScheduleSaving(false);
  };

  // DND inline block messages: { [lead.id]: true } — auto-cleared after 4s
  const [dndBlocked, setDndBlocked] = useState({});
  const showDndBlock = (leadId) => {
    setDndBlocked(p => ({ ...p, [leadId]: true }));
    setTimeout(() => setDndBlocked(p => { const n = { ...p }; delete n[leadId]; return n; }), 4000);
  };

  // Dial wrappers that pre-check DND before proceeding
  const handleDialWithDndCheck = async (lead, campaignId) => {
    try {
      const res = await apiFetch(`${API_URL}/dnd/check/${encodeURIComponent(lead.phone)}`);
      if (res.ok) {
        const data = await res.json();
        if (data.is_dnd) { showDndBlock(lead.id); return; }
      }
    } catch (_) {}
    onCampaignDial(lead, campaignId, browserAccountId);
  };

  const handleWebCallWithDndCheck = async (lead, campaignId) => {
    // If call is already active for this lead, let End Call through without DND check
    if (webCallActive === lead.id) { onCampaignWebCall(lead, campaignId); return; }
    try {
      const res = await apiFetch(`${API_URL}/dnd/check/${encodeURIComponent(lead.phone)}`);
      if (res.ok) {
        const data = await res.json();
        if (data.is_dnd) { showDndBlock(lead.id); return; }
      }
    } catch (_) {}
    onCampaignWebCall(lead, campaignId);
  };

  // Auto-dismiss success toast after 4 s
  useEffect(() => {
    if (qaStatus?.type === 'success') {
      const t = setTimeout(() => setQaStatus(null), 4000);
      return () => clearTimeout(t);
    }
  }, [qaStatus]);

  const fetchInsights = async () => {
    setInsightsLoading(true);
    setInsightsError('');
    try {
      const params = new URLSearchParams();
      if (detailExecutiveFilter?.length) params.set('executive_ids', detailExecutiveFilter.join(','));
      const query = params.toString() ? `?${params.toString()}` : '';
      const [insightsRes, reviewsRes] = await Promise.all([
        apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/call-insights${query}`),
        apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/call-reviews${query}`),
      ]);
      if (!insightsRes.ok) {
        setCallInsights(null);
        setInsightsError(`Insights endpoint returned ${insightsRes.status}`);
      } else {
        setCallInsights(await insightsRes.json());
      }
      if (!reviewsRes.ok) {
        setCallReviews([]);
        if (!insightsError) setInsightsError(`Reviews endpoint returned ${reviewsRes.status}`);
      } else {
        setCallReviews(await reviewsRes.json());
      }
    } catch (e) {
      console.error('Failed to fetch insights', e);
      setInsightsError('Network error loading call insights');
    }
    setInsightsLoading(false);
  };

  const fetchRetries = async () => {
    setRetriesLoading(true);
    try {
      const params = new URLSearchParams();
      if (detailExecutiveFilter?.length) params.set('executive_ids', detailExecutiveFilter.join(','));
      const query = params.toString() ? `?${params.toString()}` : '';
      const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/retries${query}`);
      const data = await res.json();
      setRetries(Array.isArray(data) ? data : (data?.retries || []));
    } catch (e) { console.error('Failed to fetch retries', e); }
    setRetriesLoading(false);
  };

  useEffect(() => {
    if (detailTab === 'calllog') fetchCallLog(selectedCampaign.id, detailExecutiveFilter);
    if (detailTab === 'insights') fetchInsights();
    if (detailTab === 'retries') fetchRetries();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detailTab, selectedCampaign.id, detailExecutiveFilter]);

  // Load call outcome stats whenever the campaign detail is opened.
  useEffect(() => {
    if (!selectedCampaign?.id) return;
    const load = async () => {
      try {
        const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/call-outcome-stats`);
        if (res.ok) setCallOutcomeStats(await res.json());
      } catch (e) { console.error('Failed to load call outcome stats', e); }
    };
    load();
  }, [selectedCampaign.id, apiFetch, API_URL]);

  useEffect(() => {
    const fetchBilling = async () => {
      try {
        const res = await apiFetch(`${API_URL}/billing/usage`);
        const data = await res.json();
        if (data && data.has_subscription) setBillingUsage(data);
      } catch { /* no subscription — ignore */  }
    };
    fetchBilling();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── Exotel account selector state ─────────────────────────────────────────
  const [campaignDefaultAccount, setCampaignDefaultAccount] = useState(null);
  const [exotelAccountSaveStatus, setExotelAccountSaveStatus] = useState('idle'); // idle | saving | saved | error

  const [humanCallLead, setHumanCallLead] = useState(null); // lead being human-called
  const [humanCallPhone, setHumanCallPhone] = useState(() => localStorage.getItem('humanCallAgentPhone') || '');
  const [humanCallStatus, setHumanCallStatus] = useState('idle'); // idle | dialing | done | error
  const [humanCallError, setHumanCallError] = useState('');

  // const [twilioBrowserLead, setTwilioBrowserLead] = useState(null); // lead for Twilio WebRTC call

  // Call-action visibility from Settings page (localStorage).
  const [visibleCallActions, setVisibleCallActions] = useState({
    dial: true,
    browserCall: true,
    simWebCall: true,
  });
  useEffect(() => {
    if (hideAiFeatures) {
      setVisibleCallActions({ dial: false, browserCall: true, simWebCall: false });
      return;
    }
    try {
      const saved = JSON.parse(localStorage.getItem('callified_call_actions') || '{}');
      setVisibleCallActions({
        dial: saved.dial !== false,
        browserCall: saved.browserCall !== false,
        simWebCall: saved.simWebCall !== false,
      });
    } catch { /* ignore */ }
  }, [hideAiFeatures]);

  useEffect(() => {
    if (selectedCampaign.channel === 'whatsapp') return;
    // Fetch all org accounts
    apiFetch(`${API_URL}/exotel-accounts/options`)
      .then(r => r.ok ? r.json() : [])
      .then(data => setOrgExotelAccounts(Array.isArray(data) ? data : []))
      .catch(() => {});
    // Fetch which account is linked to this campaign
    apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/exotel-account`)
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (data?.exotel_account_id) setSelectedExotelAccountId(String(data.exotel_account_id));
        if (data?.account) setCampaignDefaultAccount(data.account);
      })
      .catch(() => {});
    // Restore per-machine browser-call account from localStorage only when the user
    // is allowed to change it. Otherwise the fixed campaign/lead assignment account is used.
    if (canChangeBrowserCallAccount) {
      try {
        const saved = localStorage.getItem(browserAccountKey(selectedCampaign.id));
        if (saved != null) setBrowserAccountId(saved);
      } catch { /* ignore */ }
    } else {
      setBrowserAccountId('');
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCampaign.id, browserAccountKey]);

  // If a browser-call account is already saved in localStorage for this campaign
  // and it differs from the server-side campaign default, push it to the server
  // so AI auto-dial and API calls use the same account without requiring the
  // user to re-select it manually.
  useEffect(() => {
    if (!selectedCampaign.id || !browserAccountId) return;
    if (String(browserAccountId) === String(selectedExotelAccountId)) return;
    const accountId = parseInt(browserAccountId, 10);
    if (!accountId) return;
    setSelectedExotelAccountId(browserAccountId);
    const chosen = findCallingAccount(browserAccountId);
    if (chosen) setCampaignDefaultAccount(chosen);
    apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/exotel-account`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ exotel_account_id: accountId }),
    }).catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedCampaign.id, browserAccountId]);

  const callingAccountOptions = mergeProviderAccount(orgExotelAccounts, campaignDefaultAccount)
    .filter(a => (a.direction || 'outbound') !== 'inbound')
    .filter(a => a.provider === 'tata' || a.app_type === 'voicebot');
  const findCallingAccount = (id) => callingAccountOptions.find(a => String(a.id) === String(id))
    || orgExotelAccounts.find(a => String(a.id) === String(id) && (a.direction || 'outbound') !== 'inbound');
  const campaignDefaultLabel = campaignDefaultAccount
    ? `Use campaign default ([${campaignDefaultAccount.provider === 'tata' ? 'Tata Tele' : 'Exotel'}] ${campaignDefaultAccount.name} · ${campaignDefaultAccount.caller_id})`
    : 'Use campaign default';

  const handleSaveExotelAccount = async () => {
    setExotelAccountSaveStatus('saving');
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/exotel-account`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ exotel_account_id: selectedExotelAccountId ? parseInt(selectedExotelAccountId) : 0 }),
      });
      setExotelAccountSaveStatus(res.ok ? 'saved' : 'error');
    } catch { setExotelAccountSaveStatus('error'); }
    setTimeout(() => setExotelAccountSaveStatus('idle'), 2000);
  };

  const scoreColor = (s) => {
    if (s >= 4) return T.green;
    if (s >= 3) return T.amber;
    if (s >= 2) return '#f97316';
    return T.red;
  };

  const sentimentColor = (s) => {
    if (s === 'positive') return T.green;
    if (s === 'neutral') return '#60a5fa';
    if (s === 'negative') return '#f97316';
    if (s === 'annoyed') return T.red;
    return T.muted;
  };

  const reviewByTranscript = {};
  callReviews.forEach(r => { reviewByTranscript[r.transcript_id] = r; });

  // ── shared mini styles ──────────────────────────────────────────
  const btnPrimary = {
    background: T.accent, border: 'none', color: '#fff',
    borderRadius: 8, padding: '8px 18px', cursor: 'pointer',
    fontSize: 13, fontWeight: 600, fontFamily: T.font,
  };
  const btnGhost = {
    background: '#fff', border: `1px solid ${T.border}`, color: T.sub,
    borderRadius: 8, padding: '6px 14px', cursor: 'pointer',
    fontSize: 12, fontWeight: 600, fontFamily: T.font,
  };
  const inputStyle = {
    padding: '7px 10px', border: `1px solid ${T.border}`, borderRadius: 8,
    fontSize: 13, fontFamily: T.font, color: T.text, background: '#fff', outline: 'none',
  };
  const thStyle = {
    padding: '10px 14px', fontSize: 11, fontWeight: 600, color: T.muted,
    textTransform: 'uppercase', letterSpacing: '0.06em', textAlign: 'left',
    borderBottom: `1px solid ${T.border}`, background: T.bg,
  };
  const tdStyle = { padding: '11px 14px', fontSize: 13, color: T.sub, borderBottom: `1px solid ${T.border}` };

  return (
    <div style={{ padding: '24px 28px', background: T.bg, minHeight: '100%', fontFamily: T.font }}>

      {/* Back */}
      <button onClick={handleBack}
        style={{ background: 'none', border: 'none', color: T.accent, cursor: 'pointer', fontSize: '0.85rem', fontWeight: 600, marginBottom: '1.25rem', padding: 0, fontFamily: T.font }}>
        ← Back to Campaigns
      </button>

      {/* Campaign header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: '1.5rem', flexWrap: 'wrap' }}>
        <h2 style={{ margin: 0, fontSize: 22, fontWeight: 700, color: T.text }}>{selectedCampaign.name}</h2>
        {selectedCampaign.product_id > 0 ? (
          <span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: '#0891b2', background: 'rgba(8,145,178,0.1)' }}>
            {getProductName(selectedCampaign.product_id)}
          </span>
        ) : (
          <span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: T.amber, background: 'rgba(245,158,11,0.1)' }}>
            ⚠ No product linked
          </span>
        )}
        {statusBadge(selectedCampaign.status)}
        {canEditCampaign && (
          <button onClick={() => handleEditCampaign(selectedCampaign)}
            style={{ background: 'rgba(245,158,11,0.08)', border: `1px solid rgba(245,158,11,0.3)`, color: '#92400e', borderRadius: 8, padding: '5px 14px', cursor: 'pointer', fontSize: 12, fontWeight: 600, fontFamily: T.font }}>
            Edit Campaign
          </button>
        )}
        {canEditCampaign && (
          <select className="form-input" value={selectedCampaign.lead_source || ''}
            onChange={async (e) => {
              const src = e.target.value;
              await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}`, {
                method: 'PUT', headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ lead_source: src })
              });
              setSelectedCampaign({...selectedCampaign, lead_source: src});
            }}
            style={{ width: 'auto', height: 32, fontSize: '0.8rem', padding: '4px 10px', background: '#fff', border: `1px solid ${T.border}`, color: T.text, borderRadius: 8, fontFamily: T.font }}>
            <option value="">No Source</option>
            <option value="facebook">Facebook / Meta</option>
            <option value="google">Google Ads</option>
            <option value="instagram">Instagram</option>
            <option value="linkedin">LinkedIn</option>
            <option value="website">Website</option>
            <option value="referral">Referral</option>
            <option value="cold">Cold Outreach</option>
          </select>
        )}
      </div>

      {/* Metrics grid */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 12, marginBottom: 16 }}>
        {[
          { label: 'Total Leads', val: stats.total, color: T.accent },
          { label: 'Called', val: stats.called, color: T.sub },
          { label: 'Remaining', val: stats.remaining, color: T.amber },
          { label: 'Qualified', val: stats.qualified, color: T.pink },
          { label: 'Appointments', val: stats.booked, color: T.green },
        ].map(s => (
          <div key={s.label} style={{ ...card, padding: '18px 20px' }}>
            <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>{s.label}</div>
            <div style={{ fontSize: 28, fontWeight: 700, fontFamily: T.mono, color: s.color }}>{s.val}</div>
          </div>
        ))}
      </div>

      {/* Call outcome stats */}
      <div style={{ ...card, padding: '16px 20px', marginBottom: 16 }}>
        <div style={{ fontSize: 12, color: T.muted, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 12 }}>Call Outcomes</div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(5,1fr)', gap: 12 }}>
          {[
            { label: 'Total Calls', val: callOutcomeStats.total, color: T.accent },
            { label: 'Connected', val: callOutcomeStats.connected, color: T.green },
            { label: 'Completed', val: callOutcomeStats.completed, color: '#0891b2' },
            { label: 'Unanswered', val: callOutcomeStats.unanswered, color: T.amber },
            { label: 'Busy / Failed', val: callOutcomeStats.busy + callOutcomeStats.failed, color: T.red },
          ].map(s => (
            <div key={s.label} style={{ textAlign: 'center' }}>
              <div style={{ fontSize: 24, fontWeight: 700, fontFamily: T.mono, color: s.color }}>{s.val}</div>
              <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>{s.label}</div>
            </div>
          ))}
        </div>
      </div>

      {/* Voice Settings — hidden for WhatsApp campaigns and AI-hidden users */}
      {selectedCampaign.channel !== 'whatsapp' && !hideAiFeatures && (
        <div style={{ ...card, marginBottom: 16, padding: '14px 18px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <span style={{ fontSize: 12, color: T.muted, fontWeight: 700, whiteSpace: 'nowrap', textTransform: 'uppercase', letterSpacing: '0.05em' }}>🔊 Voice Settings</span>
            <select className="form-input" value={campVoice.tts_provider}
              onChange={e => { const p = e.target.value; setCampVoice(v => ({...v, tts_provider: p, tts_voice_id: (INDIAN_VOICES[p] || [])[0]?.id || ''})); }}
              style={{ ...inputStyle, height: 32, minWidth: 110 }}>
              <option value="">-- Provider --</option>
              <option value="elevenlabs">ElevenLabs</option>
              <option value="sarvam">Sarvam AI</option>
              <option value="smallest">Smallest AI</option>
            </select>
            <select className="form-input" value={campVoice.tts_voice_id}
              onChange={e => setCampVoice(v => ({...v, tts_voice_id: e.target.value}))}
              style={{ ...inputStyle, height: 32, minWidth: 160 }}>
              <option value="">-- Voice --</option>
              {(() => {
                const recs = VOICE_RECOMMENDATIONS[campVoice.tts_language]?.[campVoice.tts_provider]?.top || [];
                const voices = INDIAN_VOICES[campVoice.tts_provider] || [];
                const recommended = voices.filter(v => recs.includes(v.id));
                const others = voices.filter(v => !recs.includes(v.id));
                return (<>
                  {recommended.length > 0 && <optgroup label="★ Recommended">
                    {recommended.map(v => <option key={v.id} value={v.id}>★ {v.name}</option>)}
                  </optgroup>}
                  {recommended.length > 0 && <optgroup label="All Voices">
                    {others.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
                  </optgroup>}
                  {recommended.length === 0 && voices.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
                </>);
              })()}
            </select>
            <select className="form-input" value={campVoice.tts_language}
              onChange={e => setCampVoice(v => ({...v, tts_language: e.target.value}))}
              style={{ ...inputStyle, height: 32, minWidth: 100 }}>
              <option value="">-- Language --</option>
              {INDIAN_LANGUAGES.map(l => (
                <option key={l.code} value={l.code}>{l.name}</option>
              ))}
            </select>
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 12, color: T.muted, fontWeight: 700, whiteSpace: 'nowrap' }}>
              Max Call Time
              <input
                className="form-input"
                type="number"
                min="0"
                max="60"
                step="1"
                value={campVoice.max_call_duration_seconds ? Math.round(Number(campVoice.max_call_duration_seconds) / 60) : ''}
                onChange={e => {
                  const minutes = Math.max(0, Math.min(60, Number(e.target.value || 0)));
                  setCampVoice(v => ({ ...v, max_call_duration_seconds: minutes ? minutes * 60 : 0 }));
                }}
                placeholder="No limit"
                style={{ ...inputStyle, height: 32, width: 92 }}
              />
              min
            </label>
            {canSaveVoiceSettings && <button style={{
                background: campVoiceSaveStatus === 'saved' ? T.green
                  : campVoiceSaveStatus === 'error' ? T.red
                  : T.accent,
                border: 'none', color: '#fff', fontSize: 12, padding: '6px 14px', borderRadius: 8,
                cursor: campVoiceSaveStatus === 'saving' ? 'wait' : 'pointer', whiteSpace: 'nowrap',
                opacity: campVoiceSaveStatus === 'saving' ? 0.7 : 1, fontWeight: 600, fontFamily: T.font,
              }}
              disabled={campVoiceSaveStatus === 'saving'}
              onClick={handleSaveCampVoice}>
              {campVoiceSaveStatus === 'saving' ? 'Saving…'
                : campVoiceSaveStatus === 'saved' ? '✓ Saved'
                : campVoiceSaveStatus === 'error' ? '✗ Failed'
                : 'Save'}
            </button>}
            {canSaveVoiceSettings && <button style={{ ...btnGhost, fontSize: 12 }} onClick={handleResetCampVoice}>Reset to Org Default</button>}
          </div>
          <div style={{ fontSize: '0.7rem', color: T.accent, marginTop: 6 }}>
            {campVoice.tts_provider
              ? (() => {
                  const providerLabel = campVoice.tts_provider === 'elevenlabs' ? 'ElevenLabs'
                    : campVoice.tts_provider === 'sarvam' ? 'Sarvam AI'
                    : 'Smallest AI';
                  const voiceLabel = (INDIAN_VOICES[campVoice.tts_provider] || [])
                    .find(v => v.id === campVoice.tts_voice_id)?.name
                    || campVoice.tts_voice_id || 'none';
                  const langLabel = INDIAN_LANGUAGES
                    .find(l => l.code === campVoice.tts_language)?.name
                    || campVoice.tts_language;
                  const maxMinutes = Number(campVoice.max_call_duration_seconds || 0) / 60;
                  return `Current: ${providerLabel} - ${voiceLabel}` + (langLabel ? ` (${langLabel})` : '') + (maxMinutes > 0 ? ` · Max call time ${Math.round(maxMinutes)} min` : ' · No max limit');
                })()
              : 'Using org default'}
          </div>
          {VOICE_RECOMMENDATIONS[campVoice.tts_language]?.[campVoice.tts_provider]?.note && (
            <div style={{ fontSize: '0.65rem', color: '#0891b2', marginTop: 4 }}>
              ℹ {VOICE_RECOMMENDATIONS[campVoice.tts_language][campVoice.tts_provider].note}
            </div>
          )}
        </div>
      )}

      {/* Browser Call Account (per-machine) — hidden for WhatsApp campaigns */}
      {selectedCampaign.channel !== 'whatsapp' && (
        <div style={{ ...card, marginBottom: 16, padding: '14px 18px' }}>
          <div style={{ fontSize: 12, color: T.muted, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: 10 }}>
            🖥️ Browser Call Account (this machine)
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <select
              className="form-input"
              value={effectiveBrowserAccountId}
              disabled={!canChangeBrowserCallAccount}
              onChange={e => {
                if (!canChangeBrowserCallAccount) return;
                const v = e.target.value;
                const override = mustSelectBrowserCallAccount ? v : (v === selectedExotelAccountId ? '' : v);
                setBrowserAccountId(override);
                try {
                  localStorage.setItem(browserAccountKey(selectedCampaign.id), override);
                } catch { /* ignore */ }
                // Selecting a browser-call account also makes it the campaign
                // default so AI auto-dial and external API calls use the same
                // provider account (e.g. Tata Tele) instead of falling back to
                // the campaign's previously linked Exotel account.
                if (v) {
                  const accountId = parseInt(v, 10);
                  setSelectedExotelAccountId(v);
                  const chosen = findCallingAccount(v);
                  if (chosen) setCampaignDefaultAccount(chosen);
                  apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/exotel-account`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ exotel_account_id: accountId || 0 }),
                  }).catch(() => {});
                }
              }}
              style={{ ...inputStyle, height: 34, minWidth: 280, maxWidth: 420, opacity: canChangeBrowserCallAccount ? 1 : 0.6, cursor: canChangeBrowserCallAccount ? 'pointer' : 'not-allowed' }}>
              <option value="">{campaignDefaultLabel}</option>
              {callingAccountOptions.map(a => (
                <option key={a.id} value={String(a.id)}>
                  [{a.provider === 'tata' ? 'Tata Tele' : 'Exotel'}] {a.name} · {a.caller_id}
                </option>
              ))}
            </select>
          </div>
          <div style={{ fontSize: '0.7rem', color: T.muted, marginTop: 6 }}>
            {effectiveBrowserAccount
              ? (() => {
                  const a = effectiveBrowserAccount;
                  const source = browserAccountId ? 'browser override' : 'campaign default';
                  return `Dialing from: ${a.name || a.account_sid} · ${a.account_sid} · ${a.caller_id || 'no caller ID'} (${source})`;
                })()
              : orgExotelAccounts.length === 0
                ? 'No saved voicebot accounts — go to More → Provider Accounts to add one'
                : canChangeBrowserCallAccount
                  ? 'Select a browser call account before calling. This choice is saved only in this browser.'
                  : 'This account is fixed by the campaign/lead assignment. Contact admin to change it.'}
          </div>
        </div>
      )}

      {/* Billing Minutes Widget */}
      {billingUsage && (
        <div style={{
          display: 'inline-flex', alignItems: 'center', gap: 10,
          background: 'rgba(99,102,241,0.06)', border: `1px solid rgba(99,102,241,0.2)`,
          borderRadius: 20, padding: '6px 16px', marginBottom: 14,
        }}>
          <span style={{ fontSize: '0.8rem', color: T.sub, fontWeight: 600, whiteSpace: 'nowrap' }}>
            ⏱ {billingUsage.minutes_remaining} / {billingUsage.minutes_included} min remaining
          </span>
          <div style={{ width: 80, height: 6, background: T.border, borderRadius: 3, overflow: 'hidden' }}>
            <div style={{
              width: `${Math.min(100, (billingUsage.minutes_used / billingUsage.minutes_included) * 100)}%`,
              height: '100%', borderRadius: 3,
              background: (billingUsage.minutes_used / billingUsage.minutes_included) > 0.9
                ? T.red : (billingUsage.minutes_used / billingUsage.minutes_included) > 0.7
                ? T.amber : T.accent,
              transition: 'width 0.5s ease',
            }} />
          </div>
        </div>
      )}

      {/* Live Dial Events Feed — AI dialer events; hide for AI-hidden users */}
      {!hideAiFeatures && <div style={{ ...card, marginBottom: 14, padding: 14, maxHeight: 200, overflowY: 'auto' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
          <span style={{ fontSize: 11, color: T.muted, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '1px' }}>📡 Live Campaign Activity</span>
          {liveEvents.length > 0 && (
            <button onClick={() => {
              setLiveEvents([]);
              try {
                localStorage.setItem(`liveEventsClearedAt:${selectedCampaign.id}`, String(Date.now()));
              } catch { /* ignore */ }
            }} style={{ background: 'none', border: 'none', color: T.muted, cursor: 'pointer', fontSize: '0.7rem', fontFamily: T.font }}>Clear</button>
          )}
        </div>
        {liveEvents.length === 0 ? (
          <div style={{ fontSize: '0.75rem', color: T.muted, fontStyle: 'italic', padding: '4px 0' }}>
            Listening for new events… start a dial to see activity here.
          </div>
        ) : (
          liveEvents.map((ev, i) => (
            <div key={i} style={{ fontSize: '0.8rem', color: T.sub, padding: '3px 0', borderBottom: `1px solid ${T.border}`, fontFamily: T.mono }}>
              {withDate(ev?.label, ev?.ts)}
            </div>
          ))
        )}
      </div>}

      {/* Quick Add Lead Form */}
      {canCreateLead && <div style={{ ...card, padding: '12px 16px', marginBottom: 14, display: 'flex', gap: 8, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <span style={{ fontSize: 12, color: T.muted, fontWeight: 700, height: 32, display: 'flex', alignItems: 'center', textTransform: 'uppercase', letterSpacing: '0.05em' }}>➕ Quick Add:</span>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <input className="form-input" placeholder="Name" value={qaName}
            onChange={e => {
              const v = e.target.value;
              setQaName(v);
              const t = v.trim();
              if (!t) setQaNameErr('');
              else if (!/[A-Za-z]/.test(t)) setQaNameErr('Name must contain at least one letter');
              else setQaNameErr('');
            }}
            style={{ ...inputStyle, width: 130, height: 32, border: qaNameErr ? `1px solid ${T.red}` : `1px solid ${T.border}` }} />
          {qaNameErr && <span style={{ color: T.red, fontSize: '0.7rem', marginTop: 4 }}>{qaNameErr}</span>}
        </div>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <input className="form-input" placeholder="Phone (e.g. 9876543210)" value={qaPhone}
            inputMode="tel"
            onChange={e => {
              const v = e.target.value;
              setQaPhone(v);
              if (v.trim() && !isValidPhone(v)) {
                setQaPhoneErr(PHONE_VALIDATION_MESSAGE);
              } else {
                setQaPhoneErr('');
              }
            }}
            style={{ ...inputStyle, width: 180, height: 32, border: qaPhoneErr ? `1px solid ${T.red}` : `1px solid ${T.border}` }} />
          {qaPhoneErr && <span style={{ color: T.red, fontSize: '0.7rem', marginTop: 4 }}>{qaPhoneErr}</span>}
        </div>
        <button style={{ ...btnPrimary, height: 32, padding: '4px 14px' }}
          onClick={async () => {
            const name = qaName.trim();
            const phone = qaPhone.trim();
            const nameErr = !name
              ? 'Name is required'
              : (!/[A-Za-z]/.test(name) ? 'Name must contain at least one letter' : '');
            const phoneErr = !phone
              ? 'Phone is required'
              : (!isValidPhone(phone) ? PHONE_VALIDATION_MESSAGE : '');
            setQaNameErr(nameErr);
            setQaPhoneErr(phoneErr);
            setQaApiErr('');
            if (nameErr || phoneErr) return;
            try {
              const res = await apiFetch(`${API_URL}/leads`, {
                method: 'POST', headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ first_name: name, phone: normalizePhone(phone), source: 'Manual' })
              });
              const data = await res.json();
              let leadId = data.id;
              const errMsg = data.error || data.message || '';
              const isDuplicate = res.status === 409 || errMsg.includes('already exists');
              if (data.fields && typeof data.fields === 'object') {
                if (data.fields.first_name) setQaNameErr(data.fields.first_name);
                if (data.fields.phone) setQaPhoneErr(data.fields.phone);
                if (!isDuplicate) return;
              }
              if (!leadId && isDuplicate) {
                const searchRes = await apiFetch(`${API_URL}/leads/search?q=${encodeURIComponent(phone)}`);
                const found = await searchRes.json();
                if (Array.isArray(found) && found.length > 0) leadId = found[0].id;
              }
              if (leadId) {
                await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/leads`, {
                  method: 'POST', headers: {'Content-Type': 'application/json'},
                  body: JSON.stringify({ lead_ids: [leadId] })
                });
                setQaName('');
                setQaPhone('');
                fetchCampaignLeads(selectedCampaign.id);
                fetchCampaigns();
              } else if (!data.fields) { setQaApiErr(errMsg || `Error (${res.status})`); }
            } catch(e) { setQaApiErr('Failed: ' + (e?.message || 'network error')); }
          }}>Add & Assign</button>
        {qaApiErr && <span style={{ color: T.red, fontSize: '0.75rem', width: '100%', marginTop: 4 }}>{qaApiErr}</span>}
      </div>}

      {selectedCampaign.channel === 'whatsapp' && !hideAiFeatures && (
        <div style={{ marginBottom: 14 }}>
          <WhatsAppBlastPanel campaignId={selectedCampaign.id} apiFetch={apiFetch} API_URL={API_URL} />
        </div>
      )}

      {/* Action buttons */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, flexWrap: 'wrap' }}>
        {canAssignLeads && <button style={{ ...btnPrimary }} onClick={() => { setSelectedLeadIds([]); setShowAddLeadsModal(true); }}>+ Add from CRM</button>}
        {canImportLeads && <button style={{ ...btnPrimary, background: '#0891b2' }}
          onClick={() => { setCsvFile(null); setShowCsvImportModal(true); }}>📤 Import CSV</button>}
        {canExportLeads && <button
          style={{ ...btnPrimary, background: T.green }}
          onClick={() => {
            downloadCSV({
              apiFetch,
              url: `${API_URL}/campaigns/${selectedCampaign.id}/export-leads`,
              filename: `leads_${(selectedCampaign.name || selectedCampaign.id).toString().replace(/\s+/g,'_')}.csv`,
              toast,
            });
          }}>
          ⬇ Export
        </button>}
        {!hideAiFeatures && canDialAll && campaignLeads.some(l => (l.status || '').toLowerCase() === 'new') && (
          <button style={{ ...btnPrimary, background: T.green }}
            onClick={async () => {
              if (!requireSelectedDialAccount()) return;
              const newCount = (campaignLeads || []).filter(l => (l.status || '').toLowerCase() === 'new').length;
              if (!await confirm({ message: `Dial ALL ${newCount} new leads? (30s gap between calls)` })) return;
              try {
                const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/dial-all`, {
                  method: 'POST',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify({ exotel_account_id: parseInt(browserAccountId, 10) || 0 }),
                });
                const data = await res.json();
                toast(data.message || 'Dialing started');
                const ri = setInterval(() => { fetchCampaignLeads(selectedCampaign.id); fetchCallLog(selectedCampaign.id); }, 15000);
                setTimeout(() => clearInterval(ri), 30 * 60 * 1000);
              } catch { toast('Dial failed');  }
            }}>
            📞 Dial All New ({(campaignLeads || []).filter(l => (l.status || '').toLowerCase() === 'new').length})
          </button>
        )}
        {!hideAiFeatures && canDialAll && <button style={{ ...btnPrimary, background: '#7c3aed' }}
          onClick={async () => {
            if (!requireSelectedDialAccount()) return;
            if (!await confirm({ message: `Dial ALL ${campaignLeads.length} leads? (30s gap)` })) return;
            try {
              const res = await apiFetch(`${API_URL}/campaigns/${selectedCampaign.id}/dial-all?force=true`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ exotel_account_id: parseInt(browserAccountId, 10) || 0 }),
              });
              const data = await res.json();
              toast(data.message || 'Dialing started');
              const ri = setInterval(() => { fetchCampaignLeads(selectedCampaign.id); fetchCallLog(selectedCampaign.id); }, 15000);
              setTimeout(() => clearInterval(ri), 30 * 60 * 1000);
            } catch { toast('Failed');  }
          }}>
          📞 Dial All ({campaignLeads.length})
        </button>}
        {selectedCampaign.channel !== 'whatsapp' && canAutoDial && canBrowserCall && visibleCallActions.browserCall && (
          <button
            style={{
              ...btnPrimary,
              background: autoDialEnabled ? '#f59e0b' : '#475569',
              display: 'flex', alignItems: 'center', gap: 6,
            }}
            onClick={() => {
              if (!requireSelectedDialAccount()) return;
              const next = !autoDialEnabled;
              setAutoDialEnabled(next);
              if (next) {
                const queueLeads = selectedAutoDialLeads.length > 0 ? selectedAutoDialLeads : paginatedLeads;
                setAutoDialSelectedOnly(selectedAutoDialLeads.length > 0);
                setAutoDialQueue(queueLeads.map(l => l.id));
                toast(selectedAutoDialLeads.length > 0
                  ? `Auto dial enabled for ${selectedAutoDialLeads.length} selected lead(s). Start a browser call to begin.`
                  : 'Auto dial enabled. Start a browser call to begin.');
              } else {
                setAutoDialActiveId(null);
                setAutoDialQueue([]);
                setAutoDialSelectedOnly(false);
                toast('Auto dial stopped');
              }
            }}
            title={autoDialActiveId ? 'Stop auto-dialing' : 'After a browser call ends, automatically dial the next filtered lead'}>
            {autoDialActiveId
              ? '⏹ Stop Auto Dial'
              : autoDialEnabled
                ? '⏸ Auto Dial On'
                : `▶ Auto Dial${autoDialButtonCount ? ` (${autoDialButtonCount})` : ''}`}
          </button>
        )}
        {detailTab === 'leads' && canAssignLeads && !autoDialEnabled && (
          <div style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            {(bulkSelectedIds.size > 0 || bulkSelectAll) && (
              <span style={{ fontSize: 12, fontWeight: 700, color: T.sub, whiteSpace: 'nowrap' }}>
                {bulkSelectAll ? `${campaignLeadsTotal} selected` : `${bulkSelectedIds.size} selected`}
              </span>
            )}
            {bulkSelectedIds.size > 0 && !bulkSelectAll && campaignLeadsTotal > paginatedLeads.length && (
              <button
                onClick={selectAllCampaign}
                disabled={bulkAssigning}
                style={{ ...btnGhost, fontSize: 12, padding: '8px 10px', color: T.accent, borderColor: T.accent }}
              >
                Select all {campaignLeadsTotal}
              </button>
            )}
            <button
              type="button"
              onClick={() => {
                if (bulkSelectedIds.size === 0 && !bulkSelectAll) return;
                setShowBulkAssignMenu(v => !v);
              }}
              disabled={bulkAssigning || (bulkSelectedIds.size === 0 && !bulkSelectAll)}
              style={{
                ...btnPrimary,
                background: T.accent,
                opacity: (bulkAssigning || (bulkSelectedIds.size === 0 && !bulkSelectAll)) ? 0.55 : 1,
                cursor: (bulkAssigning || (bulkSelectedIds.size === 0 && !bulkSelectAll)) ? 'not-allowed' : 'pointer',
              }}
              title={(bulkSelectedIds.size === 0 && !bulkSelectAll) ? 'Select leads first' : 'Assign selected leads to an executive'}
            >
              {bulkAssigning ? 'Assigning...' : 'Assign Executive ▾'}
            </button>
            {showBulkAssignMenu && (
              <div style={{
                position: 'absolute',
                top: 'calc(100% + 6px)',
                left: 0,
                zIndex: 60,
                minWidth: 230,
                maxHeight: 280,
                overflowY: 'auto',
                background: '#fff',
                border: `1px solid ${T.border}`,
                borderRadius: 8,
                boxShadow: '0 14px 36px rgba(15, 23, 42, 0.18)',
                padding: 6,
              }}>
                {executives.length === 0 && (
                  <div style={{ padding: '9px 10px', fontSize: 13, color: T.muted }}>No executives found</div>
                )}
                {executives.map(e => (
                  <button
                    key={e.id}
                    type="button"
                    onClick={() => handleBulkAssignExecutive(String(e.id))}
                    style={{
                      width: '100%',
                      display: 'block',
                      textAlign: 'left',
                      background: 'transparent',
                      border: 'none',
                      borderRadius: 6,
                      padding: '9px 10px',
                      color: T.text,
                      fontSize: 13,
                      fontWeight: 600,
                      cursor: 'pointer',
                      fontFamily: T.font,
                    }}
                  >
                    {e.name || e.full_name || e.email || `Executive ${e.id}`}
                  </button>
                ))}
                <div style={{ height: 1, background: T.border, margin: '6px 0' }} />
                <button
                  type="button"
                  onClick={() => handleBulkAssignExecutive('clear')}
                  style={{
                    width: '100%',
                    display: 'block',
                    textAlign: 'left',
                    background: 'transparent',
                    border: 'none',
                    borderRadius: 6,
                    padding: '9px 10px',
                    color: T.red,
                    fontSize: 13,
                    fontWeight: 700,
                    cursor: 'pointer',
                    fontFamily: T.font,
                  }}
                >
                  Clear Assignment
                </button>
              </div>
            )}
            {(bulkSelectedIds.size > 0 || bulkSelectAll) && (
              <button
                type="button"
                onClick={clearBulkSelection}
                disabled={bulkAssigning}
                style={{ ...btnGhost, opacity: bulkAssigning ? 0.65 : 1 }}
              >
                Clear selection
              </button>
            )}
          </div>
        )}
      </div>

      {/* Auto Dial Panel — replaces tables/lists so no phone numbers are shown */}
      {autoDialEnabled && (
        <AutoDialPanel
          autoDialEnabled={autoDialEnabled}
          autoDialQueue={autoDialQueue}
          autoDialActiveId={autoDialActiveId}
          autoDialUninterrupted={autoDialUninterrupted}
          onToggleUninterrupted={setAutoDialUninterrupted}
          paginatedLeads={paginatedLeads}
          autoDialLeads={autoDialSelectedOnly ? selectedAutoDialLeads : paginatedLeads}
          browserCallLead={browserCallLead}
          browserCallDialing={browserCallDialing}
          onStart={startBrowserCallWithAutoDial}
          onStop={() => {
            setAutoDialEnabled(false);
            setAutoDialActiveId(null);
            setAutoDialQueue([]);
            setAutoDialSelectedOnly(false);
            setAutoDialUninterrupted(false);
            toast('Auto dial stopped');
          }}
          campaignName={selectedCampaign.name}
        />
      )}

      {/* Agent filter for detail tabs */}
      {!autoDialEnabled && canShowAgentFilter && detailExecutiveFilter && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 12, color: T.muted, fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Filter by agent</span>
          <div style={{ position: 'relative' }}>
            <button
              onClick={() => setShowDetailExecFilter(v => !v)}
              style={{
                padding: '7px 12px', border: `1px solid ${T.border}`, borderRadius: 8,
                fontSize: 13, fontFamily: T.font, color: T.text, background: '#fff',
                cursor: 'pointer', minWidth: 160, textAlign: 'left'
              }}>
              {detailExecutiveFilter.length === 0 ? 'All agents' : `${detailExecutiveFilter.length} agent${detailExecutiveFilter.length > 1 ? 's' : ''}`} ▾
            </button>
            {showDetailExecFilter && (
              <div style={{
                position: 'absolute', top: 'calc(100% + 6px)', left: 0, minWidth: 220,
                background: '#fff', border: `1px solid ${T.border}`, borderRadius: 8,
                boxShadow: '0 8px 24px rgba(0,0,0,0.10)', padding: '8px 10px', zIndex: 50,
                maxHeight: 300, overflowY: 'auto'
              }}>
                <input
                  type="text"
                  placeholder="Search agents..."
                  value={detailExecSearch}
                  onChange={e => setDetailExecSearch(e.target.value)}
                  onClick={e => e.stopPropagation()}
                  style={{
                    width: '100%', boxSizing: 'border-box', padding: '6px 8px', marginBottom: 6,
                    border: `1px solid ${T.border}`, borderRadius: 6, fontSize: 13, fontFamily: T.font,
                    outline: 'none'
                  }}
                />
                <div
                  onClick={() => setDetailExecutiveFilter([])}
                  style={{
                    padding: '6px 8px', borderRadius: 6, cursor: 'pointer', fontSize: 13,
                    color: detailExecutiveFilter.length === 0 ? T.accent : T.text, fontWeight: detailExecutiveFilter.length === 0 ? 700 : 400,
                    background: detailExecutiveFilter.length === 0 ? 'rgba(99,102,241,0.08)' : 'transparent'
                  }}>
                  All agents
                </div>
                {(() => {
                  const q = detailExecSearch.trim().toLowerCase();
                  const filtered = q ? (agents || []).filter(e => (e.name || e.full_name || e.email || '').toLowerCase().includes(q)) : (agents || []);
                  if (filtered.length === 0) {
                    return <div style={{ color: T.muted, fontSize: 12, padding: '6px 0' }}>No agents found.</div>;
                  }
                  return filtered.map(e => {
                    const checked = detailExecutiveFilter.includes(e.id);
                    return (
                      <label key={e.id} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0', color: T.text, fontSize: 13, cursor: 'pointer' }}>
                        <input type="checkbox" checked={checked}
                          onChange={() => setDetailExecutiveFilter(prev => checked ? prev.filter(id => id !== e.id) : [...prev, e.id])} />
                        {e.name || e.full_name || e.email}
                      </label>
                    );
                  });
                })()}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Search + Tab Switcher */}
      {!autoDialEnabled && (<>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', background: T.bg, border: `1px solid ${T.border}`, borderRadius: 8, padding: 3, gap: 2, width: 'fit-content' }}>
          {[
            { id: 'leads',   label: `👥 Leads (${campaignLeadsTotal})`,   activeColor: T.accent, hidden: !hasPermission('crm.view') },
          { id: 'calllog', label: `📞 Call Log (${callLog.length})`,       activeColor: T.green, hidden: !canViewTranscripts && !canViewRecordings },
          { id: 'insights',label: '📊 Call Insights',                      activeColor: '#a855f7', hidden: hideAiFeatures || !canViewReports },
          { id: 'retries', label: '🔄 Retries',                            activeColor: T.amber,  hidden: hideAiFeatures || !canViewReports },
          ].filter(tab => !tab.hidden).map(tab => (
            <button key={tab.id}
              onClick={() => setDetailTab(tab.id)}
              style={{
                padding: '6px 18px', borderRadius: 6, border: 'none', cursor: 'pointer',
                fontSize: 13, fontWeight: 600, fontFamily: T.font,
                background: detailTab === tab.id ? tab.activeColor : 'transparent',
                color: detailTab === tab.id ? '#fff' : T.muted,
                transition: 'all 0.15s',
              }}>
              {tab.label}
            </button>
          ))}
        </div>
        <input
          type="text"
          placeholder="Search leads by name, phone, company or source..."
          value={leadSearch}
          onChange={e => setLeadSearch(e.target.value)}
          style={{
            padding: '7px 12px', border: `1px solid ${T.border}`, borderRadius: 8,
            fontSize: 13, fontFamily: T.font, color: T.text, background: '#fff',
            outline: 'none', minWidth: 260,
          }}
        />
        {canShowAgentFilter && executives && executives.length > 0 && (
          <div style={{ position: 'relative' }}>
            <button
              onClick={() => setShowExecFilter(v => !v)}
              style={{
                padding: '7px 12px', border: `1px solid ${T.border}`, borderRadius: 8,
                fontSize: 13, fontFamily: T.font, color: T.text, background: '#fff',
                cursor: 'pointer', minWidth: 160, textAlign: 'left'
              }}>
              {execFilter.length === 0 ? 'Filter by Executive' : `${execFilter.length} executive${execFilter.length > 1 ? 's' : ''}`} ▾
            </button>
            {showExecFilter && (
              <div style={{
                position: 'absolute', top: 'calc(100% + 6px)', right: 0, minWidth: 220,
                background: '#fff', border: `1px solid ${T.border}`, borderRadius: 8,
                boxShadow: '0 8px 24px rgba(0,0,0,0.10)', padding: '8px 10px', zIndex: 50,
                maxHeight: 300, overflowY: 'auto'
              }}>
                <input
                  type="text"
                  placeholder="Search executives..."
                  value={execSearch}
                  onChange={e => setExecSearch(e.target.value)}
                  onClick={e => e.stopPropagation()}
                  style={{
                    width: '100%', boxSizing: 'border-box', padding: '6px 8px', marginBottom: 6,
                    border: `1px solid ${T.border}`, borderRadius: 6, fontSize: 13, fontFamily: T.font,
                    outline: 'none'
                  }}
                />
                {(() => {
                  const q = execSearch.trim().toLowerCase();
                  const filtered = q ? (executives || []).filter(e => (e.name || '').toLowerCase().includes(q)) : (executives || []);
                  if (filtered.length === 0) {
                    return <div style={{ color: T.muted, fontSize: 12, padding: '6px 0' }}>No executives found.</div>;
                  }
                  return filtered.map(e => {
                    const checked = execFilter.includes(String(e.id));
                    return (
                      <label key={e.id} style={{display: 'flex', alignItems: 'center', gap: 8, padding: '5px 0', color: T.text, fontSize: 13, cursor: 'pointer'}}>
                        <input type="checkbox" checked={checked}
                          onChange={() => {
                            const val = String(e.id);
                            setExecFilter(prev => checked ? prev.filter(id => id !== val) : [...prev, val]);
                          }} />
                        {e.name}
                      </label>
                    );
                  });
                })()}
              </div>
            )}
          </div>
        )}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <input
            type="datetime-local"
            value={scheduleFrom}
            onChange={e => setScheduleFrom(e.target.value)}
            style={{
              padding: '7px 10px', border: `1px solid ${T.border}`, borderRadius: 8,
              fontSize: 12, fontFamily: T.font, color: T.text, background: '#fff', outline: 'none'
            }}
          />
          <span style={{ color: T.muted, fontSize: 12, fontWeight: 600 }}>to</span>
          <input
            type="datetime-local"
            value={scheduleTo}
            onChange={e => setScheduleTo(e.target.value)}
            style={{
              padding: '7px 10px', border: `1px solid ${T.border}`, borderRadius: 8,
              fontSize: 12, fontFamily: T.font, color: T.text, background: '#fff', outline: 'none'
            }}
          />
          <button
            onClick={() => { setScheduleFrom(''); setScheduleTo(''); }}
            disabled={!scheduleFrom && !scheduleTo}
            style={{
              padding: '7px 12px', border: `1px solid ${T.border}`, borderRadius: 8,
              fontSize: 12, fontFamily: T.font, color: T.text, background: '#fff',
              cursor: (!scheduleFrom && !scheduleTo) ? 'not-allowed' : 'pointer',
              opacity: (!scheduleFrom && !scheduleTo) ? 0.5 : 1,
            }}>
            Clear
          </button>
        </div>
      </div>

      {/* Call Log Tab — WhatsApp notice */}
      {detailTab === 'calllog' && selectedCampaign.channel === 'whatsapp' && (
        <div style={{ ...card, padding: '1.5rem', marginBottom: '1.5rem', textAlign: 'center', color: T.muted }}>
          💬 Conversation history is in the <strong style={{ color: T.wa }}>WhatsApp Comms</strong> tab.
        </div>
      )}

      {/* Call Log Table */}
      {detailTab === 'calllog' && selectedCampaign.channel !== 'whatsapp' && (
        <div style={{ ...card, overflowX: 'auto', marginBottom: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'flex-end', padding: '10px 16px 0' }}>
            {canViewRecordings && <a
              href={(() => {
                const params = new URLSearchParams();
                if (detailExecutiveFilter?.length) params.set('executive_ids', detailExecutiveFilter.join(','));
                return `${API_URL}/campaigns/${selectedCampaign.id}/export-recordings${params.toString() ? `?${params.toString()}` : ''}`;
              })()}
              download
              onClick={e => {
                e.preventDefault();
                const params = new URLSearchParams();
                if (detailExecutiveFilter?.length) params.set('executive_ids', detailExecutiveFilter.join(','));
                downloadCSV({
                  apiFetch,
                  url: `${API_URL}/campaigns/${selectedCampaign.id}/export-recordings${params.toString() ? `?${params.toString()}` : ''}`,
                  filename: `recordings_${selectedCampaign.name?.replace(/\s+/g,'_') || selectedCampaign.id}.csv`,
                  toast,
                });
              }}
              style={{
                display: 'inline-flex', alignItems: 'center', gap: 6,
                background: T.green, color: '#fff', borderRadius: 7,
                padding: '6px 16px', fontSize: '0.8rem', fontWeight: 600,
                fontFamily: T.font, textDecoration: 'none', cursor: 'pointer',
              }}>
              ⬇ Export CSV
            </a>}
          </div>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Lead','Phone','Source','Time','Outcome','Quality','Duration','Recording'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {callLog.length === 0 ? (
                <tr><td colSpan="8" style={{ ...tdStyle, textAlign: 'center', color: T.muted, padding: '2rem' }}>No calls made yet.</td></tr>
              ) : callLog.map(call => {
                const review = reviewByTranscript[call.id];
                const outcomeColors = {
                  'Completed': T.green, 'Connected': '#60a5fa', 'No Answer': T.amber,
                  'Busy': '#f97316', 'Failed': T.red, 'DND Blocked': '#dc2626'
                };
                const outcomeBg = {
                  'Completed': 'rgba(16,185,129,0.1)', 'Connected': 'rgba(96,165,250,0.1)', 'No Answer': 'rgba(245,158,11,0.1)',
                  'Busy': 'rgba(249,115,22,0.1)', 'Failed': 'rgba(239,68,68,0.1)', 'DND Blocked': 'rgba(220,38,38,0.1)'
                };
                return (
                  <tr key={call.id}>
                    <td style={{ ...tdStyle, fontWeight: 600, color: T.text }}>{call.first_name} {call.last_name || ''}</td>
                    <td style={{ ...tdStyle, fontFamily: T.mono, fontSize: '0.85rem' }}>{call.phone}</td>
                    <td style={tdStyle}><span style={{ fontSize: 11, fontWeight: 600, padding: '2px 10px', borderRadius: 20, color: T.accent, background: `${T.accent}15` }}>{call.source || '-'}</span></td>
                    <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.muted }}>{formatDateTime(call.created_at, orgTimezone)}</td>
                    <td style={tdStyle}>
                      <span style={{
                        padding: '3px 10px', borderRadius: 20, fontSize: '0.75rem', fontWeight: 600,
                        color: outcomeColors[call.outcome] || T.muted,
                        background: outcomeBg[call.outcome] || 'rgba(156,163,175,0.1)',
                        border: `1px solid ${(outcomeColors[call.outcome] || T.muted)}30`
                      }}>
                        {call.outcome === 'Completed' && '✅ '}
                        {call.outcome === 'Connected' && '📞 '}
                        {call.outcome === 'No Answer' && '❌ '}
                        {call.outcome === 'Busy' && '📵 '}
                        {call.outcome === 'Failed' && '⚠️ '}
                        {call.outcome === 'DND Blocked' && '🚫 '}
                        {call.outcome}
                      </span>
                    </td>
                    <td style={tdStyle}>
                      {review ? (() => {
                        const q = Math.max(0, Math.min(5, Math.round(Number(review.quality_score) || 0)));
                        return (
                          <span style={{
                            padding: '2px 8px', borderRadius: 10, fontSize: '0.75rem', fontWeight: 700,
                            color: scoreColor(q), background: `${scoreColor(q)}18`, border: `1px solid ${scoreColor(q)}40`
                          }}>
                            {'★'.repeat(q)}{'☆'.repeat(5 - q)}
                          </span>
                        );
                      })() : (
                        <span style={{ color: T.muted, fontSize: '0.75rem' }}>--</span>
                      )}
                    </td>
                    <td style={{ ...tdStyle, fontFamily: T.mono }}>
                      {call.call_duration_s > 0 ? `${Math.floor(call.call_duration_s / 60)}:${String(Math.floor(call.call_duration_s % 60)).padStart(2, '0')}` : '-'}
                    </td>
                    <td style={tdStyle}>
                      {call.recording_url ? (
                        <AuthAudio preload="none" src={call.recording_url} className="call-log-audio" style={{ height: 36, width: 260 }} />
                      ) : (
                        <span style={{ color: T.muted, fontSize: '0.8rem' }}>—</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Call Insights Tab */}
      {detailTab === 'insights' && (
        <div style={{ marginBottom: '1.5rem' }}>
          {insightsLoading ? (
            <div style={{ ...card, padding: '2rem', textAlign: 'center', color: T.muted }}>Loading insights...</div>
          ) : insightsError ? (
            <div style={{ ...card, padding: '2rem', textAlign: 'center', color: T.red, border: `1px solid #fca5a5` }}>
              <div style={{ fontWeight: 600, marginBottom: 6 }}>Call Insights are temporarily unavailable</div>
              <div style={{ fontSize: '0.8rem', color: T.muted }}>{insightsError}</div>
            </div>
          ) : !callInsights || callInsights.total_reviews === 0 ? (
            <div style={{ ...card, padding: '2rem', textAlign: 'center', color: T.muted }}>No call reviews yet. Reviews are generated automatically after each call.</div>
          ) : (
            <>
              {/* Summary Cards */}
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 12, marginBottom: 16 }}>
                <div style={{ ...card, padding: '18px 20px' }}>
                  <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Avg Quality Score</div>
                  <div style={{ fontSize: 28, fontWeight: 700, fontFamily: T.mono, color: scoreColor(Math.round(callInsights.avg_quality_score)) }}>{callInsights.avg_quality_score}/5</div>
                </div>
                <div style={{ ...card, padding: '18px 20px' }}>
                  <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Appointment Rate</div>
                  <div style={{ fontSize: 28, fontWeight: 700, fontFamily: T.mono, color: callInsights.appointment_rate > 30 ? T.green : T.amber }}>{callInsights.appointment_rate}%</div>
                </div>
                <div style={{ ...card, padding: '18px 20px' }}>
                  <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Calls Analyzed</div>
                  <div style={{ fontSize: 28, fontWeight: 700, fontFamily: T.mono, color: T.text }}>{callInsights.total_reviews}</div>
                </div>
                <div style={{ ...card, padding: '18px 20px' }}>
                  <div style={{ fontSize: 11, color: T.muted, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.06em', marginBottom: 8 }}>Top Sentiment</div>
                  <div style={{ fontSize: 20, fontWeight: 700, color: sentimentColor(Object.entries(callInsights.sentiment_breakdown || {}).sort((a,b)=>b[1]-a[1])[0]?.[0]) }}>
                    {Object.entries(callInsights.sentiment_breakdown || {}).sort((a,b)=>b[1]-a[1])[0]?.[0] || '-'}
                  </div>
                </div>
              </div>

              {/* Improvement Suggestions */}
              {callInsights.top_improvements && callInsights.top_improvements.length > 0 && (
                <div style={{ ...card, padding: 18, marginBottom: 14 }}>
                  <div style={{ fontSize: 11, color: '#a855f7', fontWeight: 700, marginBottom: 10, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Prompt Improvement Suggestions</div>
                  {callInsights.top_improvements.map((imp, i) => (
                    <div key={i} style={{ padding: '8px 12px', marginBottom: 6, background: 'rgba(168,85,247,0.06)', borderRadius: 8, borderLeft: '3px solid #a855f7', fontSize: '0.85rem', color: T.sub }}>
                      {imp.suggestion}
                      <span style={{ color: T.muted, fontSize: '0.75rem', marginLeft: 8 }}>({imp.count}x)</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Top Failure Reasons */}
              {callInsights.top_failure_reasons && callInsights.top_failure_reasons.length > 0 && (
                <div style={{ ...card, padding: 18, marginBottom: 14 }}>
                  <div style={{ fontSize: 11, color: '#f97316', fontWeight: 700, marginBottom: 10, textTransform: 'uppercase', letterSpacing: '0.5px' }}>Top Failure Reasons</div>
                  {callInsights.top_failure_reasons.map((fr, i) => (
                    <div key={i} style={{ padding: '8px 12px', marginBottom: 6, background: 'rgba(249,115,22,0.06)', borderRadius: 8, borderLeft: '3px solid #f97316', fontSize: '0.85rem', color: T.sub }}>
                      {fr.reason}
                      <span style={{ color: T.muted, fontSize: '0.75rem', marginLeft: 8 }}>({fr.count}x)</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Per-Call Reviews Table */}
              <div style={{ ...card, overflowX: 'auto' }}>
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                  <thead>
                    <tr>
                      {['Lead','Quality','Appt Booked','Date / Time','Sentiment','What Went Well','What Went Wrong','Failure Reason'].map(h => (
                        <th key={h} style={thStyle}>{h}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {callReviews.map(r => (
                      <tr key={r.id}>
                        <td style={{ ...tdStyle, fontWeight: 600, color: T.text }}>{r.first_name} {r.last_name || ''}</td>
                        <td style={tdStyle}>
                          {(() => {
                            const q = Math.max(0, Math.min(5, Math.round(Number(r.quality_score) || 0)));
                            return (
                              <span style={{ fontWeight: 700, color: scoreColor(q), fontSize: '0.9rem' }}>
                                {'★'.repeat(q)}{'☆'.repeat(5 - q)}
                              </span>
                            );
                          })()}
                        </td>
                        <td style={tdStyle}>
                          <span style={{
                            padding: '2px 10px', borderRadius: 20, fontSize: '0.75rem', fontWeight: 600,
                            color: r.appointment_booked ? T.green : '#f97316',
                            background: r.appointment_booked ? 'rgba(16,185,129,0.1)' : 'rgba(249,115,22,0.1)',
                          }}>
                            {r.appointment_booked ? 'Yes' : 'No'}
                          </span>
                        </td>
                        <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.muted, whiteSpace: 'nowrap' }}>{formatDateTime(r.created_at, orgTimezone)}</td>
                        <td style={tdStyle}><span style={{ color: sentimentColor(r.customer_sentiment), fontWeight: 600, fontSize: '0.85rem' }}>{r.customer_sentiment}</span></td>
                        <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.muted, maxWidth: 200 }}>{r.what_went_well || '-'}</td>
                        <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.red, maxWidth: 200 }}>{r.what_went_wrong || '-'}</td>
                        <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.muted, maxWidth: 200 }}>{r.failure_reason || '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
      )}

      {/* Retries Tab */}
      {detailTab === 'retries' && (
        <div style={{ marginBottom: '1.5rem' }}>
          {retriesLoading ? (
            <div style={{ ...card, padding: '2rem', textAlign: 'center', color: T.muted }}>Loading retry queue...</div>
          ) : retries.length === 0 ? (
            <div style={{ ...card, padding: '2rem', textAlign: 'center', color: T.muted }}>No retries queued for this campaign.</div>
          ) : (
            <div style={{ ...card, overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    {['Lead','Phone','Attempt','Retry Time','Status'].map(h => (
                      <th key={h} style={thStyle}>{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {retries.map(r => {
                    const retryStatusColors = {
                      pending:   { color: T.amber, bg: 'rgba(245,158,11,0.1)',  border: 'rgba(245,158,11,0.3)' },
                      dialing:   { color: '#60a5fa', bg: 'rgba(96,165,250,0.1)', border: 'rgba(96,165,250,0.3)' },
                      completed: { color: T.green,  bg: 'rgba(16,185,129,0.1)', border: 'rgba(16,185,129,0.3)' },
                      exhausted: { color: T.red,    bg: 'rgba(239,68,68,0.1)',  border: 'rgba(239,68,68,0.3)' },
                    };
                    const sc = retryStatusColors[r.status] || retryStatusColors.pending;
                    return (
                      <tr key={r.id}>
                        <td style={{ ...tdStyle, fontWeight: 600, color: T.text }}>{r.first_name || r.lead_name || '-'} {r.last_name || ''}</td>
                        <td style={{ ...tdStyle, fontFamily: T.mono, fontSize: '0.85rem' }}>{r.phone}</td>
                        <td style={{ ...tdStyle, fontWeight: 600 }}>{r.attempt || r.attempt_number || 1}/{r.max_attempts || 3}</td>
                        <td style={{ ...tdStyle, fontSize: '0.8rem', color: T.muted }}>{r.retry_time ? formatDateTime(r.retry_time, orgTimezone) : '-'}</td>
                        <td style={tdStyle}>
                          <span style={{
                            padding: '3px 10px', borderRadius: 20, fontSize: '0.75rem', fontWeight: 600,
                            color: sc.color, background: sc.bg, border: `1px solid ${sc.border}`,
                          }}>
                            {r.status}
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Leads Table */}
      {detailTab === 'leads' && (
        <div style={{ ...card, overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {canAssignLeads && <th key="select" style={{ ...thStyle, width: 52, textAlign: 'center', position: 'relative' }}>
                  <button
                    type="button"
                    onClick={() => setShowBulkSelectMenu(v => !v)}
                    title="Bulk select leads"
                    style={{
                      width: 28,
                      height: 28,
                      borderRadius: 7,
                      border: `1px solid ${(bulkSelectedIds.size > 0 || bulkSelectAll) ? T.accent : T.border}`,
                      background: (bulkSelectedIds.size > 0 || bulkSelectAll) ? 'rgba(99,102,241,0.12)' : '#fff',
                      color: (bulkSelectedIds.size > 0 || bulkSelectAll) ? T.accent : T.sub,
                      cursor: bulkSelectionLoading ? 'wait' : 'pointer',
                      fontSize: 15,
                      fontWeight: 800,
                      lineHeight: 1,
                    }}
                    disabled={bulkSelectionLoading}
                  >
                    {bulkSelectionLoading ? '…' : '☑'}
                  </button>
                  {showBulkSelectMenu && (
                    <div style={{
                      position: 'absolute',
                      top: 46,
                      left: 10,
                      zIndex: 80,
                      width: 250,
                      background: '#fff',
                      border: '1px solid rgba(199, 210, 254, 0.95)',
                      borderRadius: 10,
                      boxShadow: '0 16px 34px rgba(15, 23, 42, 0.14)',
                      padding: 8,
                      textAlign: 'left',
                      textTransform: 'none',
                      letterSpacing: 0,
                    }}>
                      <button
                        type="button"
                        onClick={() => selectAllVisible(true)}
                        style={{ width: '100%', padding: '10px 12px', border: 'none', borderRadius: 8, background: 'transparent', textAlign: 'left', cursor: 'pointer', fontWeight: 700, color: T.text, fontSize: 14, fontFamily: T.font }}
                      >
                        This page ({paginatedLeads.length})
                      </button>
                      <button
                        type="button"
                        onClick={selectAllCampaign}
                        style={{ width: '100%', padding: '10px 12px', border: 'none', borderRadius: 8, background: 'transparent', textAlign: 'left', cursor: 'pointer', fontWeight: 700, color: T.text, fontSize: 14, fontFamily: T.font }}
                      >
                        All leads ({campaignLeadsTotal})
                      </button>
                      <div style={{ height: 1, background: T.border, margin: '7px 0 9px' }} />
                      <label style={{ display: 'block', fontSize: 12, color: T.muted, fontWeight: 700, margin: '0 0 7px 2px' }}>
                        First leads
                      </label>
                      <div style={{ display: 'flex', gap: 6 }}>
                        <input
                          className="form-input"
                          type="number"
                          min="1"
                          max={campaignLeadsTotal || undefined}
                          placeholder="100"
                          value={bulkSelectLimit}
                          onChange={e => setBulkSelectLimit(e.target.value)}
                          style={{ ...inputStyle, height: 36, fontSize: 14, padding: '7px 9px', minWidth: 0, flex: 1 }}
                        />
                        <button
                          type="button"
                          onClick={() => fetchBulkLeadSelection(bulkSelectLimit)}
                          disabled={!bulkSelectLimit || bulkSelectionLoading}
                          style={{ ...btnPrimary, padding: '8px 12px', opacity: (!bulkSelectLimit || bulkSelectionLoading) ? 0.55 : 1 }}
                        >
                          Select
                        </button>
                      </div>
                      {(bulkSelectedIds.size > 0 || bulkSelectAll) && (
                        <button
                          type="button"
                          onClick={clearBulkSelection}
                          style={{ width: '100%', marginTop: 8, padding: '10px 12px', border: 'none', borderRadius: 8, background: 'rgba(239,68,68,0.08)', textAlign: 'left', cursor: 'pointer', fontWeight: 700, color: T.red, fontSize: 14, fontFamily: T.font }}
                        >
                          Clear selection
                        </button>
                      )}
                    </div>
                  )}
                </th>}
                {['Name','Phone','Company','Source','Executive','Status','Action'].map(h => (
                  <th key={h} style={thStyle}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {campaignLeadsTotal === 0 ? (
                <tr><td colSpan={canAssignLeads ? 8 : 7} style={{ ...tdStyle, textAlign: 'center', color: T.muted, padding: '2rem' }}>{(leadSearch.trim() || execFilter.length > 0) ? 'No leads match your filters.' : 'No leads in this campaign yet. Add some to start dialing!'}</td></tr>
              ) : paginatedLeads.map(lead => (
                <React.Fragment key={lead.id}>
                  <tr>
                    {canAssignLeads && <td style={{ ...tdStyle, textAlign: 'center', verticalAlign: 'middle' }}>
                      <input
                        type="checkbox"
                        checked={bulkSelectedIds.has(lead.id)}
                        onChange={() => toggleBulkSelection(lead.id)}
                        style={{ width: 16, height: 16, accentColor: T.accent, cursor: 'pointer' }}
                      />
                    </td>}
                    <td style={{ ...tdStyle, fontWeight: 600, color: T.text }}>
                      <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 6 }}>
                        <span>{lead.first_name} {lead.last_name}</span>
                        {lead.follow_up_note && (
                          <span
                            title={lead.follow_up_note}
                            style={{
                              fontSize: 10, fontWeight: 600, fontFamily: T.font,
                              padding: '2px 8px', borderRadius: 12,
                              background: 'rgba(168,85,247,0.12)', color: '#6b21a8',
                              border: '1px solid rgba(168,85,247,0.25)',
                              cursor: 'help', maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                            }}
                          >
                            📝 {lead.follow_up_note.slice(0, 24)}{lead.follow_up_note.length > 24 ? '…' : ''}
                          </span>
                        )}
                      </div>
                    </td>
                    <td style={{ ...tdStyle, fontFamily: T.mono }}>{lead.phone}</td>
                    <td style={tdStyle}>{lead.company || '-'}</td>
                    <td style={tdStyle}>
                      {canEditLead ? <select className="form-input" value={lead.source || ''}
                        onChange={async e => {
                          const src = e.target.value;
                          try {
                            await apiFetch(`${API_URL}/leads/${lead.id}/source`, {
                              method: 'PUT',
                              headers: { 'Content-Type': 'application/json' },
                              body: JSON.stringify({ source: src })
                            });
                            fetchCampaignLeads(selectedCampaign.id);
                          } catch (err) { toast('Failed to update source'); }
                        }}
                        style={{ ...inputStyle, height: 30, fontSize: '0.8rem', padding: '2px 8px', minWidth: 120, background: '#fff' }}>
                        <option value="">No Source</option>
                        {['facebook','google','instagram','linkedin','website','referral','cold'].map(s => (
                          <option key={s} value={s}>{s[0].toUpperCase() + s.slice(1)}</option>
                        ))}
                      </select> : (lead.source || 'No Source')}
                    </td>
                    <td style={tdStyle}>
                      {canAssignLeads ? <select className="form-input" value={lead.executive_id || ''}
                        onChange={async e => {
                          const execId = e.target.value ? parseInt(e.target.value, 10) : 0;
                          if (!currentCampaignId) {
                            toast('Campaign is still loading. Please try again.');
                            return;
                          }
                          try {
                            const res = await apiFetch(`${API_URL}/leads/${lead.id}/executive`, {
                              method: 'PUT',
                              headers: { 'Content-Type': 'application/json' },
                              body: JSON.stringify({ executive_id: execId, campaign_id: currentCampaignId })
                            });
                            if (!res.ok) {
                              const data = await res.json().catch(() => ({}));
                              throw new Error(data.error || 'Failed to assign executive');
                            }
                            fetchCampaignLeads(currentCampaignId);
                          } catch (err) { toast(err.message || 'Failed to assign executive'); }
                        }}
                        style={{ ...inputStyle, height: 30, fontSize: '0.8rem', padding: '2px 8px', minWidth: 120 }}>
                        <option value="">— Unassigned —</option>
                        {executives.map(e => <option key={e.id} value={e.id}>{e.name}</option>)}
                      </select> : (
                        executiveNameForLead(lead)
                      )}
                    </td>
                    <td style={tdStyle}>
                      {canEditLead ? <select className="form-input" value={lead.status || 'New'}
                        onChange={e => handleLeadStatusChange(lead.id, e.target.value)}
                        style={{ ...inputStyle, height: 30, fontSize: '0.8rem', padding: '2px 8px' }}>
                        {LEAD_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
                      </select> : (lead.status || 'New')}
                    </td>
                    <td style={tdStyle}>
                      <div style={{ display: 'flex', gap: 5, flexWrap: 'wrap' }}>
                        {canEditLead && <button
                          onClick={() => handleEditLead(lead)}
                          style={{ fontSize: 11, padding: '4px 10px', cursor: 'pointer', background: 'rgba(245,158,11,0.08)', color: '#92400e', border: '1px solid rgba(245,158,11,0.25)', borderRadius: 6, fontWeight: 600, fontFamily: T.font }}>
                          ✏️ Edit
                        </button>}
                        {canDial && visibleCallActions.dial && (
                          <button
                            onClick={() => handleDialClick(lead)}
                            disabled={dialingId === lead.id || webCallActive === lead.id}
                            style={{
                              fontSize: 11, padding: '4px 10px', fontWeight: 600, fontFamily: T.font,
                              cursor: (dialingId === lead.id || webCallActive === lead.id) ? 'not-allowed' : 'pointer',
                              opacity: (dialingId === lead.id || webCallActive === lead.id) ? 0.5 : 1,
                              background: 'rgba(16,185,129,0.08)', color: '#065f46',
                              border: '1px solid rgba(16,185,129,0.25)', borderRadius: 6,
                            }}>
                            {dialingId === lead.id ? '📞 Wait...' : '📞 Dial'}
                          </button>
                        )}
                        {/* Manual Call disabled — use Browser Call instead
                        {selectedCampaign.channel !== 'whatsapp' && (
                          <button
                            onClick={() => { setHumanCallLead(lead); setHumanCallStatus('idle'); setHumanCallError(''); }}
                            style={{
                              fontSize: 11, padding: '4px 10px', fontWeight: 600, fontFamily: T.font,
                              cursor: 'pointer',
                              background: 'rgba(234,179,8,0.08)', color: '#854d0e',
                              border: '1px solid rgba(234,179,8,0.3)', borderRadius: 6,
                            }}>
                            📲 Manual Call
                          </button>
                        )} */}
                        {canBrowserCall && selectedCampaign.channel !== 'whatsapp' && visibleCallActions.browserCall && (
                          <button
                            onClick={() => startBrowserCallWithAutoDial(lead)}
                            disabled={browserCallDialing || browserCallLead != null}
                            title={autoDialEnabled ? 'Auto-dial is enabled' : 'Call from browser mic — 1x cost'}
                            style={{
                              fontSize: 11, padding: '4px 10px', fontWeight: 600, fontFamily: T.font,
                              cursor: (browserCallDialing || browserCallLead != null) ? 'not-allowed' : 'pointer',
                              opacity: (browserCallDialing || browserCallLead != null) ? 0.6 : 1,
                              background: autoDialEnabled ? 'rgba(245,158,11,0.12)' : 'rgba(99,102,241,0.08)',
                              color: autoDialEnabled ? '#b45309' : '#3730a3',
                              border: `1px solid ${autoDialEnabled ? 'rgba(245,158,11,0.35)' : 'rgba(99,102,241,0.3)'}`, borderRadius: 6,
                            }}>
                            {autoDialEnabled ? '⏩ Browser Call' : '🎙 Browser Call'}
                          </button>
                        )}
                        {canMakeCalls && selectedCampaign.channel === 'whatsapp' && (
                          <button
                            onClick={() => handleSendWA(lead)}
                            disabled={waSendingId === lead.id}
                            style={{
                              fontSize: 11, padding: '4px 10px', fontWeight: 600, fontFamily: T.font,
                              cursor: waSendingId === lead.id ? 'not-allowed' : 'pointer',
                              opacity: waSendingId === lead.id ? 0.6 : 1,
                              background: waSendStatus[lead.id] === 'sent' ? 'rgba(37,211,102,0.15)' : 'rgba(37,211,102,0.08)',
                              color: waSendStatus[lead.id] === 'error' ? '#dc2626' : '#065f46',
                              border: `1px solid ${waSendStatus[lead.id] === 'error' ? 'rgba(239,68,68,0.3)' : 'rgba(37,211,102,0.35)'}`,
                              borderRadius: 6,
                            }}>
                            {waSendingId === lead.id ? '⏳ Sending...' : waSendStatus[lead.id] === 'sent' ? '✅ Sent' : '💬 Send WA'}
                          </button>
                        )}
                        {canBrowserCall && visibleCallActions.simWebCall && (
                          <button
                            onClick={() => onCampaignWebCall(lead, selectedCampaign.id)}
                            disabled={webCallActive != null && webCallActive !== lead.id}
                            style={{
                              fontSize: 11, padding: '4px 10px', fontWeight: 600, fontFamily: T.font,
                              cursor: (webCallActive != null && webCallActive !== lead.id) ? 'not-allowed' : 'pointer',
                              opacity: (webCallActive != null && webCallActive !== lead.id) ? 0.5 : 1,
                              borderRadius: 6,
                              border: webCallActive === lead.id ? `1px solid rgba(239,68,68,0.3)` : `1px solid rgba(99,102,241,0.25)`,
                              color: webCallActive === lead.id ? T.red : T.accent,
                              background: webCallActive === lead.id ? 'rgba(239,68,68,0.08)' : 'rgba(99,102,241,0.08)',
                            }}>
                            {webCallActive === lead.id ? '🔴 End Call' : '🌐 Sim Web Call'}
                          </button>
                        )}
                        {dndBlockedLeadIds.has(lead.id) && (
                          <span title="This number is on the DND list — outbound dials are blocked"
                            style={{ fontSize: 11, padding: '4px 10px', borderRadius: 6,
                              background: '#fee2e2', color: T.red,
                              border: '1px solid #fca5a5', fontWeight: 600,
                              display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                            🚫 DND — number blocked
                          </span>
                        )}
                        {canViewTranscripts && <button
                          onClick={() => handleViewTranscripts({ ...lead, campaign_id: selectedCampaign.id })}
                          style={{ fontSize: 11, padding: '4px 10px', cursor: 'pointer', fontFamily: T.font, borderRadius: 6, fontWeight: (lead.transcript_count > 0 || lead.recording_count > 0 || lead.dial_attempts > 0) ? 600 : 400,
                            background: (lead.transcript_count > 0 || lead.recording_count > 0 || lead.dial_attempts > 0) ? 'rgba(16,185,129,0.08)' : T.bg,
                            color: (lead.transcript_count > 0 || lead.recording_count > 0 || lead.dial_attempts > 0) ? '#065f46' : T.muted,
                            border: (lead.transcript_count > 0 || lead.recording_count > 0 || lead.dial_attempts > 0) ? '1px solid rgba(16,185,129,0.25)' : `1px solid ${T.border}`,
                          }}>
                          {lead.transcript_count > 0
                            ? `📋 ${lead.transcript_count} Transcript${lead.transcript_count > 1 ? 's' : ''}`
                            : (lead.recording_count > 0 || lead.dial_attempts > 0) ? '📋 Call History' : '📋 No Calls'}
                          {lead.recording_count > 0 && ' 🔊'}
                          {lead.dial_attempts > 0 && ` (${lead.dial_attempts} dial${lead.dial_attempts > 1 ? 's' : ''})`}
                        </button>}
                        {canEditLead && <button
                          onClick={() => openNoteModal(lead)}
                          style={{ fontSize: 11, padding: '4px 10px', cursor: 'pointer', background: 'rgba(168,85,247,0.08)', color: '#6b21a8', border: '1px solid rgba(168,85,247,0.25)', borderRadius: 6, fontWeight: 600, fontFamily: T.font }}>
                          📝 Note
                        </button>}
                        {canScheduleCalls && <button
                          onClick={() => {
                            openScheduleModal(lead, false);
                          }}
                          style={{ fontSize: 11, padding: '4px 10px', cursor: 'pointer', background: 'rgba(59,130,246,0.08)', color: '#1e40af', border: '1px solid rgba(59,130,246,0.25)', borderRadius: 6, fontWeight: 600, fontFamily: T.font }}>
                          📅 Schedule
                        </button>}
                        {canDeleteLead && <button onClick={async () => {
                            const fullName = `${lead.first_name || ''} ${lead.last_name || ''}`.trim() || 'this lead';
                            const ok = await confirm({
                              title: 'Remove Lead',
                              message: `Remove "${fullName}" from this campaign? This action cannot be undone.`,
                              danger: true,
                              okText: 'Remove',
                              cancelText: 'Cancel',
                            });
                            if (ok) handleRemoveLead(lead.id);
                          }}
                          style={{ fontSize: 11, padding: '4px 10px', cursor: 'pointer',
                            background: '#fee2e2', border: '1px solid #fca5a5',
                            color: T.red, borderRadius: 6, fontWeight: 600, fontFamily: T.font }}>
                          Remove
                        </button>}
                        {canScheduleCalls && lead.has_pending_scheduled_call && lead.next_scheduled_at && (
                          <div style={{ position: 'relative', display: 'inline-flex' }}>
                            <button
                              onClick={(e) => {
                                e.stopPropagation();
                                setScheduleActionLeadId((prev) => prev === lead.id ? null : lead.id);
                              }}
                              title="Scheduled call actions"
                              style={{
                                fontSize: 11, padding: '4px 10px', borderRadius: 6,
                                background: 'rgba(59,130,246,0.12)', color: '#1e40af',
                                border: '1px solid rgba(59,130,246,0.3)', fontWeight: 600,
                                fontFamily: T.font, whiteSpace: 'nowrap', display: 'inline-flex', alignItems: 'center', gap: 8,
                                cursor: 'pointer'
                              }}>
                              <span>📅 {formatDateTime(lead.next_scheduled_at, orgTimezone)}</span>
                              <span style={{ fontSize: 10, opacity: 0.85 }}>▾</span>
                            </button>
                            {scheduleActionLeadId === lead.id && (
                              <div
                                onClick={(e) => e.stopPropagation()}
                                style={{
                                  position: 'absolute', top: 'calc(100% + 6px)', right: 0, minWidth: 140,
                                  background: '#ffffff', border: '1px solid #dbe4ff', borderRadius: 10,
                                  boxShadow: '0 10px 26px rgba(15,23,42,0.12)', padding: 6, zIndex: 20,
                                  display: 'flex', flexDirection: 'column', gap: 4
                                }}>
                                <button
                                  onClick={() => {
                                    setScheduleActionLeadId(null);
                                    openScheduleModal(lead, true);
                                  }}
                                  style={{
                                    textAlign: 'left', padding: '8px 10px', borderRadius: 8, border: 'none',
                                    background: 'transparent', color: '#1e40af', cursor: 'pointer',
                                    fontSize: 12, fontWeight: 600, fontFamily: T.font
                                  }}>
                                  Edit
                                </button>
                                <button
                                  onClick={async () => {
                                    setScheduleActionLeadId(null);
                                    try {
                                      const ok = await confirm({
                                        title: 'Cancel Scheduled Call',
                                        message: `Cancel the scheduled call for ${lead.first_name || 'this lead'}?`,
                                        danger: true,
                                        okText: 'Delete',
                                        cancelText: 'Keep It',
                                      });
                                      if (!ok) return;
                                      const res = await apiFetch(`${API_URL}/scheduled-calls/${lead.scheduled_call_id}`, { method: 'DELETE' });
                                      if (!res.ok) throw new Error('Failed to cancel scheduled call');
                                      toast('Scheduled call cancelled');
                                      fetchCampaignLeads(selectedCampaign.id);
                                      refreshScheduledCalls?.();
                                    } catch (err) {
                                      toast(err?.message || 'Delete failed');
                                    }
                                  }}
                                  style={{
                                    textAlign: 'left', padding: '8px 10px', borderRadius: 8, border: 'none',
                                    background: 'rgba(239,68,68,0.08)', color: '#dc2626', cursor: 'pointer',
                                    fontSize: 12, fontWeight: 600, fontFamily: T.font
                                  }}>
                                  Delete
                                </button>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    </td>
                  </tr>
                  {!hideAiFeatures && (lead.follow_up_note || editingNote?.leadId === lead.id || generatedNote?.leadId === lead.id) && (
                    <tr>
                      <td colSpan="6" style={{ padding: '12px 24px', background: 'rgba(99,102,241,0.04)', borderLeft: `3px solid ${T.accent}`, borderBottom: `1px solid ${T.border}` }}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                          <div style={{ fontSize: '0.8rem', color: T.muted, textTransform: 'uppercase', letterSpacing: '1px', fontWeight: 600 }}>✨ AI Follow-Up Note</div>
                          {editingNote?.leadId !== lead.id && (
                            <button
                              onClick={() => handleGenerateNote(lead)}
                              disabled={noteGenerating}
                              style={{
                                background: 'rgba(99,102,241,0.08)', border: `1px solid rgba(99,102,241,0.25)`,
                                color: T.accent, borderRadius: 6, padding: '3px 12px',
                                fontSize: '0.75rem', fontWeight: 600, cursor: noteGenerating ? 'wait' : 'pointer', fontFamily: T.font,
                              }}>
                              {noteGenerating ? '⏳ Generating…' : '↺ Regenerate'}
                            </button>
                          )}
                        </div>
                        {editingNote?.leadId === lead.id ? (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                            <textarea
                              autoFocus
                              value={editingNote.text}
                              onChange={e => setEditingNote({ ...editingNote, text: e.target.value })}
                              rows={4}
                              style={{
                                width: '100%', padding: '8px 10px', borderRadius: 6,
                                border: `1px solid ${T.accent}`, fontSize: '0.85rem',
                                fontFamily: T.font, lineHeight: 1.5, resize: 'vertical',
                                outline: 'none', color: T.text, boxSizing: 'border-box',
                              }}
                            />
                            <div style={{ display: 'flex', gap: 8 }}>
                              <button onClick={() => handleSaveInlineNote(lead)} disabled={noteSaving} style={{
                                background: T.accent, color: '#fff', border: 'none', borderRadius: 6,
                                padding: '5px 16px', fontSize: '0.8rem', fontWeight: 600,
                                cursor: noteSaving ? 'wait' : 'pointer', fontFamily: T.font,
                              }}>{noteSaving ? 'Saving…' : 'Save'}</button>
                              <button onClick={() => setEditingNote(null)} style={{
                                background: 'transparent', color: T.muted, border: `1px solid ${T.border}`,
                                borderRadius: 6, padding: '5px 16px', fontSize: '0.8rem',
                                fontWeight: 600, cursor: 'pointer', fontFamily: T.font,
                              }}>Cancel</button>
                            </div>
                          </div>
                        ) : (() => {
                          const noteData = generatedNote?.leadId === lead.id ? generatedNote : null;
                          const noteText = noteData?.text || lead.follow_up_note;
                          return (
                            <div>
                              <div
                                onClick={() => setEditingNote({ leadId: lead.id, text: noteText, recordingUrl: noteData?.recordingUrl || '', recordingFilename: noteData?.recordingFilename || '' })}
                                title="Click to edit"
                                style={{
                                  whiteSpace: 'pre-wrap', color: T.sub, fontSize: '0.85rem', lineHeight: 1.5,
                                  cursor: 'text', padding: '4px 6px', borderRadius: 6, margin: '-4px -6px',
                                  border: '1px solid transparent',
                                }}
                                onMouseEnter={e => { e.currentTarget.style.border = `1px solid ${T.border}`; e.currentTarget.style.background = '#fff'; }}
                                onMouseLeave={e => { e.currentTarget.style.border = '1px solid transparent'; e.currentTarget.style.background = 'transparent'; }}
                                role="button"
                                tabIndex={0}
                                onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.currentTarget.click(); } }}
                              >{linkify(noteText)}</div>
                              {noteData?.recordingUrl && (
                                <div style={{ marginTop: 8, fontSize: '0.75rem' }}>
                                  <a href={noteData.recordingUrl} target="_blank" rel="noreferrer"
                                    style={{ color: T.accent, textDecoration: 'underline', wordBreak: 'break-all' }}
                                    onClick={e => e.stopPropagation()}>
                                    {noteData.recordingUrl}
                                  </a>
                                </div>
                              )}
                            </div>
                          );
                        })()}
                      </td>
                    </tr>
                  )}
                </React.Fragment>
              ))}
            </tbody>
          </table>
          {totalPages > 1 && (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', borderTop: `1px solid ${T.border}`, fontFamily: T.font, fontSize: '0.85rem', color: T.sub }}>
              <span>Showing {(safePage - 1) * PAGE_SIZE + 1}–{Math.min(safePage * PAGE_SIZE, campaignLeadsTotal)} of {campaignLeadsTotal}</span>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <button
                  onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
                  disabled={safePage === 1}
                  style={{ minWidth: 32, height: 32, padding: '0 10px', border: `1px solid ${T.border}`, borderRadius: 6, background: '#fff', color: T.sub, cursor: safePage === 1 ? 'not-allowed' : 'pointer', opacity: safePage === 1 ? 0.5 : 1, fontFamily: T.font, fontSize: '0.8rem', fontWeight: 600 }}>
                  Prev
                </button>
                {(() => {
                  const pages = [];
                  if (totalPages <= 7) {
                    for (let i = 1; i <= totalPages; i++) pages.push(i);
                  } else {
                    pages.push(1);
                    if (safePage > 3) pages.push('...');
                    for (let i = Math.max(2, safePage - 1); i <= Math.min(totalPages - 1, safePage + 1); i++) pages.push(i);
                    if (safePage < totalPages - 2) pages.push('...');
                    pages.push(totalPages);
                  }
                  return pages.map((p, idx) =>
                    p === '...' ? (
                      <span key={`gap-${idx}`} style={{ padding: '0 4px', color: T.muted }}>...</span>
                    ) : (
                      <button
                        key={p}
                        onClick={() => setCurrentPage(p)}
                        style={{ minWidth: 32, height: 32, padding: '0 8px', border: `1px solid ${p === safePage ? T.accent : T.border}`, borderRadius: 6, background: p === safePage ? T.accent : '#fff', color: p === safePage ? '#fff' : T.sub, cursor: 'pointer', fontFamily: T.font, fontSize: '0.8rem', fontWeight: 600 }}>
                        {p}
                      </button>
                    )
                  );
                })()}
                <button
                  onClick={() => setCurrentPage(p => Math.min(totalPages, p + 1))}
                  disabled={safePage === totalPages}
                  style={{ minWidth: 32, height: 32, padding: '0 10px', border: `1px solid ${T.border}`, borderRadius: 6, background: '#fff', color: T.sub, cursor: safePage === totalPages ? 'not-allowed' : 'pointer', opacity: safePage === totalPages ? 0.5 : 1, fontFamily: T.font, fontSize: '0.8rem', fontWeight: 600 }}>
                  Next
                </button>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginLeft: 10, paddingLeft: 10, borderLeft: `1px solid ${T.border}` }}>
                  <input
                    type="number"
                    min={1}
                    max={totalPages}
                    value={jumpPage}
                    placeholder="Page #"
                    onChange={e => setJumpPage(e.target.value)}
                    onKeyDown={e => { if (e.key === 'Enter') handleJump(); }}
                    style={{ width: 70, height: 30, padding: '0 8px', border: `1px solid ${T.border}`, borderRadius: 6, fontFamily: T.font, fontSize: '0.8rem' }}
                  />
                  <button
                    onClick={handleJump}
                    style={{ height: 32, padding: '0 12px', border: `1px solid ${T.accent}`, borderRadius: 6, background: T.accent, color: '#fff', cursor: 'pointer', fontFamily: T.font, fontSize: '0.8rem', fontWeight: 600 }}>
                    Go
                  </button>
                </div>
              </div>
            </div>
          )}
        </div>
      )}
      </>)}

      {/* Schedule Call Modal */}
      {scheduleLead && (
        <div
          className="modal-overlay"
          onClick={e => { if (e.target === e.currentTarget) closeScheduleModal(); }}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
        >
          <div style={{ background: '#fff', border: `1px solid ${T.border}`, borderRadius: 16, boxShadow: '0 8px 40px rgba(0,0,0,0.12)', maxWidth: 440, width: '100%', padding: '1.5rem', fontFamily: T.font }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
              <h3 style={{ margin: 0, color: T.text, fontSize: 18, fontWeight: 700 }}>{scheduleEditingCallId ? '📅 Edit Scheduled Call' : '📅 Schedule Call'}</h3>
              <button onClick={closeScheduleModal}
                style={{ background: 'transparent', border: 'none', color: T.muted, fontSize: '1.2rem', cursor: 'pointer' }}>✕</button>
            </div>
            <p style={{ color: T.muted, fontSize: '0.85rem', marginBottom: '1.25rem' }}>
              {scheduleLead.first_name} {scheduleLead.last_name} — {scheduleLead.phone}
            </p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <label style={{ fontSize: '0.8rem', color: T.sub, fontWeight: 600 }}>
                Date &amp; Time
                <input
                  type="datetime-local"
                  className="form-input"
                  value={scheduleAt}
                  onChange={e => { setScheduleAt(e.target.value); if (scheduleStatus.kind) setScheduleStatus({ kind: '', text: '' }); }}
                  min={(() => {
                    const d = new Date();
                    const pad = n => String(n).padStart(2, '0');
                    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
                  })()}
                  style={{ ...inputStyle, width: '100%', marginTop: 6 }}
                />
              </label>
              <label style={{ fontSize: '0.8rem', color: T.sub, fontWeight: 600 }}>
                Callback mode
                <select
                  className="form-input"
                  value={scheduleMode}
                  onChange={e => setScheduleMode(e.target.value)}
                  style={{ ...inputStyle, width: '100%', marginTop: 6, height: 38 }}
                >
                  <option value="manual">Manual / Browser Callback (auto-connect for you)</option>
                  <option value="ai">AI Dial</option>
                </select>
              </label>
              <label style={{ fontSize: '0.8rem', color: T.sub, fontWeight: 600 }}>
                Notes (optional)
                <textarea
                  className="form-input"
                  value={scheduleNotes}
                  onChange={e => setScheduleNotes(e.target.value)}
                  rows={3}
                  placeholder="e.g. follow-up on pricing discussion"
                  style={{ ...inputStyle, width: '100%', marginTop: 6, resize: 'vertical' }}
                />
              </label>
            </div>
            {scheduleStatus.kind === 'error' && (
              <div style={{
                marginTop: '1rem', padding: '8px 12px', borderRadius: 8, fontSize: '0.8rem',
                background: '#fee2e2', border: '1px solid #fca5a5', color: T.red
              }}>
                ⚠️ {scheduleStatus.text}
              </div>
            )}
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: '1.25rem' }}>
              <button onClick={closeScheduleModal}
                style={{ ...btnGhost }}>
                Cancel
              </button>
              <button
                style={{ ...btnPrimary, opacity: (scheduleSaving || !scheduleAt) ? 0.6 : 1 }}
                disabled={scheduleSaving || !scheduleAt}
                onClick={async () => {
                  if (!scheduleAt) return;
                  if (new Date(scheduleAt).getTime() <= Date.now()) {
                    setScheduleStatus({ kind: 'error', text: 'Please pick a future date and time.' });
                    return;
                  }
                  setScheduleStatus({ kind: '', text: '' });
                  setScheduleSaving(true);
                  try {
                    const serverTime = new Date(scheduleAt).toISOString();
                    const res = await apiFetch(`${API_URL}/scheduled-calls`, {
                      method: 'POST',
                      headers: {'Content-Type': 'application/json'},
                      body: JSON.stringify({
                        lead_id: scheduleLead.id,
                        campaign_id: selectedCampaign.id,
                        scheduled_at: serverTime,
                        notes: scheduleNotes,
                        mode: scheduleMode,
                        executive_id: scheduleLead.executive_id || null,
                        scheduled_by_user_id: currentUser?.id || null,
                      }),
                    });
                    if (!res.ok) {
                      const data = await res.json().catch(() => ({}));
                      setScheduleStatus({ kind: 'error',
                        text: 'Failed to schedule: ' + (data.error || data.detail || res.status) });
                    } else {
                      const data = await res.json().catch(() => ({}));
                      closeScheduleModal();
                      toast(scheduleEditingCallId ? 'Scheduled call updated' : 'Call scheduled');
                      fetchCampaignLeads(selectedCampaign.id);
                      refreshScheduledCalls?.();
                      if (data.id) clearDismissedScheduledCall?.(data.id);
                    }
                  } catch { setScheduleStatus({ kind: 'error', text: 'Network error while scheduling.'  });
                  }
                  setScheduleSaving(false);
                }}>
                {scheduleSaving ? (scheduleEditingCallId ? 'Saving…' : 'Scheduling…') : (scheduleEditingCallId ? 'Save Changes' : 'Schedule Call')}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Human Call Modal */}
      {/* Browser Call Modal — Twilio WebRTC (zero delay) [disabled] */}
      {/* {twilioBrowserLead && (
        <TwilioBrowserCallModal
          lead={twilioBrowserLead}
          campaignId={selectedCampaign.id}
          callerPhone={orgExotelAccounts.find(a => String(a.id) === selectedExotelAccountId)?.caller_id || ''}
          onClose={() => setTwilioBrowserLead(null)}
        />
      )} */}

      {humanCallLead && (
        <div
          className="modal-overlay"
          onClick={e => { if (e.target === e.currentTarget) { setHumanCallLead(null); setHumanCallStatus('idle'); } }}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
        >
          <div style={{ background: '#fff', border: `1px solid ${T.border}`, borderRadius: 16, boxShadow: '0 8px 40px rgba(0,0,0,0.12)', maxWidth: 420, width: '100%', padding: '1.5rem', fontFamily: T.font }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
              <h3 style={{ margin: 0, color: T.text, fontSize: 18, fontWeight: 700 }}>📲 Manual Call</h3>
              <button onClick={() => { setHumanCallLead(null); setHumanCallStatus('idle'); }}
                style={{ background: 'transparent', border: 'none', color: T.muted, fontSize: '1.2rem', cursor: 'pointer' }}>✕</button>
            </div>
            <p style={{ color: T.muted, fontSize: '0.85rem', marginBottom: '1.25rem' }}>
              Calling <strong>{humanCallLead.first_name} {humanCallLead.last_name}</strong> — {humanCallLead.phone}
            </p>
            <p style={{ color: T.sub, fontSize: '0.8rem', marginBottom: '1rem', lineHeight: 1.5 }}>
              Exotel will call <strong>your phone</strong> first. Pick up and you&apos;ll hear the customer&apos;s name announced, then be connected to them.
            </p>
            <label style={{ fontSize: '0.8rem', color: T.sub, fontWeight: 600 }}>
              Your phone number
              <input
                type="tel"
                className="form-input"
                value={humanCallPhone}
                onChange={e => setHumanCallPhone(e.target.value)}
                placeholder="+91XXXXXXXXXX"
                style={{ ...inputStyle, width: '100%', marginTop: 6 }}
                onKeyDown={e => e.key === 'Enter' && handleHumanCallDial()}
              />
            </label>
            {humanCallStatus === 'error' && (
              <div style={{ marginTop: '0.75rem', padding: '8px 12px', borderRadius: 8, fontSize: '0.8rem', background: '#fee2e2', border: '1px solid #fca5a5', color: T.red }}>
                ⚠️ {humanCallError}
              </div>
            )}
            {humanCallStatus === 'done' && (
              <div style={{ marginTop: '0.75rem', padding: '8px 12px', borderRadius: 8, fontSize: '0.8rem', background: 'rgba(16,185,129,0.08)', border: '1px solid rgba(16,185,129,0.25)', color: '#065f46' }}>
                ✅ Dialing your phone…
              </div>
            )}
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', marginTop: '1.25rem' }}>
              <button onClick={() => { setHumanCallLead(null); setHumanCallStatus('idle'); }}
                style={{ ...btnGhost }}>
                Cancel
              </button>
              <button
                disabled={humanCallStatus === 'dialing' || humanCallStatus === 'done' || !humanCallPhone.trim()}
                onClick={handleHumanCallDial}
                style={{ ...btnPrimary, opacity: (humanCallStatus === 'dialing' || humanCallStatus === 'done' || !humanCallPhone.trim()) ? 0.6 : 1 }}>
                {humanCallStatus === 'dialing' ? '📞 Dialing…' : '📞 Call Me'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Quick Note Modal */}
      {noteModalLead && (
        <div className="modal-overlay" onClick={() => setNoteModalLead(null)} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()} style={{maxWidth: '520px'}} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
            <h2 style={{marginTop: 0, marginBottom: '0.5rem'}}>📝 Quick Note</h2>
            <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '1.5rem'}}>
              {noteModalLead.first_name} {noteModalLead.last_name} — {noteModalLead.phone}
            </p>
            <textarea className="form-input" rows={5} value={noteModalText}
              onChange={e => setNoteModalText(e.target.value)}
              placeholder="Type your follow-up note here..."
              style={{width: '100%', minHeight: '120px', resize: 'vertical', fontSize: '0.9rem', lineHeight: 1.5}} />
            <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '1.5rem'}}>
              <button onClick={() => setNoteModalLead(null)}
                style={{background: 'transparent', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', padding: '8px 18px', borderRadius: '8px', cursor: 'pointer'}}>
                Cancel
              </button>
              <button className="btn-primary" onClick={handleSaveNoteModal}
                disabled={noteModalSaving || !noteModalText.trim()}
                style={{opacity: (noteModalSaving || !noteModalText.trim()) ? 0.5 : 1, cursor: (noteModalSaving || !noteModalText.trim()) ? 'not-allowed' : 'pointer'}}>
                {noteModalSaving ? 'Saving…' : 'Save Note'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Disposition Modal (post-call, gates browser auto-dial) */}
      {showDispositionModal && dispositionLead && (
        <div
          className="modal-overlay"
          onClick={e => { if (e.target === e.currentTarget) e.stopPropagation(); }}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); } }}
        >
          <div style={{
            background: '#fff', border: `1px solid ${T.border}`, borderRadius: 16,
            boxShadow: '0 8px 40px rgba(0,0,0,0.12)', maxWidth: 480, width: '100%',
            padding: '1.5rem', fontFamily: T.font,
          }} onClick={e => e.stopPropagation()}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
              <h3 style={{ margin: 0, color: T.text, fontSize: 18, fontWeight: 700 }}>📝 Call Disposition</h3>
            </div>
            <p style={{ color: T.muted, fontSize: '0.85rem', marginBottom: '1.25rem' }}>
              {dispositionLead.first_name} {dispositionLead.last_name} — {maskPhone(dispositionLead.phone)}
            </p>

            <label style={{ display: 'block', fontSize: '0.8rem', color: T.sub, fontWeight: 600, marginBottom: '0.75rem' }}>
              Status
              <select
                className="form-input"
                value={dispositionStatus}
                onChange={e => setDispositionStatus(e.target.value)}
                style={{ ...inputStyle, width: '100%', marginTop: 6, height: 38 }}
              >
                {LEAD_STATUSES.map(s => <option key={s} value={s}>{s}</option>)}
              </select>
            </label>

            <label style={{ display: 'block', fontSize: '0.8rem', color: T.sub, fontWeight: 600, marginBottom: '0.75rem' }}>
              Remarks / Follow-up note
              <textarea
                className="form-input"
                rows={4}
                value={dispositionRemarks}
                onChange={e => setDispositionRemarks(e.target.value)}
                placeholder="Add remarks..."
                style={{ ...inputStyle, width: '100%', marginTop: 6, minHeight: 90, resize: 'vertical', fontSize: '0.9rem', lineHeight: 1.5 }}
              />
            </label>

            <label style={{ display: 'block', fontSize: '0.8rem', color: T.sub, fontWeight: 600, marginBottom: '1.25rem' }}>
              Follow-up date & time (optional)
              <input
                type="datetime-local"
                className="form-input"
                value={dispositionFollowUpAt}
                onChange={e => setDispositionFollowUpAt(e.target.value)}
                style={{ ...inputStyle, width: '100%', marginTop: 6, height: 38 }}
              />
            </label>

            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end' }}>
              <button
                onClick={() => saveDispositionAndAdvance(true)}
                disabled={dispositionSaving}
                style={{ ...btnGhost, opacity: dispositionSaving ? 0.6 : 1 }}>
                {dispositionSaving ? 'Saving…' : 'Save & Stop'}
              </button>
              <button
                onClick={() => saveDispositionAndAdvance(false)}
                disabled={dispositionSaving || !dispositionStatus.trim()}
                style={{ ...btnPrimary, opacity: (dispositionSaving || !dispositionStatus.trim()) ? 0.6 : 1 }}>
                {dispositionSaving ? 'Saving…' : (dispositionNextLead ? 'Save & Next' : 'Save & Finish')}
              </button>
            </div>
          </div>
        </div>
      )}

    </div>
  );
}
