import React, { createContext, useContext, useState, useRef, useCallback, useEffect, useMemo } from 'react';
import { API_URL } from '../constants/api';
import BrowserCallModal from '../components/campaigns/BrowserCallModal';
import ScheduledCallbackPreview from '../components/ScheduledCallbackPreview';
import { useToast } from './UIContext';
import { useAuth } from './AuthContext';
import { useOrg } from './OrgContext';
import { useVoice } from './VoiceContext';

const CallContext = createContext(null);

export function CallProvider({ children }) {
  const { apiFetch, currentUser } = useAuth();
  const { orgProducts } = useOrg();
  const { activeVoiceProvider, activeVoiceId, activeLanguage } = useVoice();
  const toast = useToast();

  const [dialingId, setDialingId] = useState(null);
  const [webCallActive, setWebCallActive] = useState(null);
  // rechargePrompt holds the backend's billing/recharge message when
  // a 402 comes back from the dial endpoints. Rendered as a themed modal
  // (matches the app's dark glass-panel UI) instead of the native browser
  // confirm() dialog, which used the OS theme and looked out of place.
  const [rechargePrompt, setRechargePrompt] = useState(null);
  const [minuteBalancePrompt, setMinuteBalancePrompt] = useState(null);
  const webCallWsRef = useRef(null);
  const webCallAudioCtxRef = useRef(null);

  // Global Browser Call state (moved out of CampaignDetail so scheduled-call
  // reminders and auto-dialer can trigger calls from anywhere).
  const [browserCallLead, setBrowserCallLead] = useState(null);
  const [browserCallCampaignId, setBrowserCallCampaignId] = useState(null);
  const [browserCallSid, setBrowserCallSid] = useState(null);
  const [browserCallDialing, setBrowserCallDialing] = useState(false);

  // Agent presence: manual override (idle/break). On-call is detected from
  // active browser / sim-web call state automatically.
  const [manualPresenceStatus, setManualPresenceStatus] = useState('idle');

  // Track scheduled callbacks that have already been auto-triggered so we
  // don't repeatedly dial the same lead while the scheduled row is still pending.
  const triggeredScheduledRef = useRef(new Set());

  // When a manual callback becomes due, show a preview with customer details,
  // previous remarks, and call history before connecting.
  const [scheduledCallbackPreview, setScheduledCallbackPreview] = useState(null);

  // Manual scheduled-call reminder notifications
  const DISMISSED_SCHEDULED_KEY = 'callified_dismissed_scheduled_calls';
  const [dueManualCalls, setDueManualCalls] = useState([]);
  const [dismissedIds, setDismissedIds] = useState(() => {
    try {
      const raw = localStorage.getItem(DISMISSED_SCHEDULED_KEY);
      return new Set(raw ? JSON.parse(raw) : []);
    } catch { return new Set(); }
  });
  useEffect(() => {
    try {
      localStorage.setItem(DISMISSED_SCHEDULED_KEY, JSON.stringify(Array.from(dismissedIds)));
    } catch { /* ignore */ }
  }, [dismissedIds]);
  const clearDismissedScheduledCall = useCallback((id) => {
    setDismissedIds(prev => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, []);
  const dismissScheduledCall = useCallback((id) => {
    setDismissedIds(prev => new Set(prev).add(id));
  }, []);
  const dueScheduledCalls = useMemo(() => dueManualCalls.filter(c => !dismissedIds.has(c.id)), [dueManualCalls, dismissedIds]);
  const browserCallEndedCbRef = useRef(null);
  const showBillingPrompt = useCallback((msg) => {
    if (/minute balance|credits? exhausted/i.test(msg || '')) {
      setMinuteBalancePrompt(msg);
    } else {
      setRechargePrompt(msg);
    }
  }, []);

  const handleDial = useCallback(async (lead) => {
    setDialingId(lead.id);
    try {
      const res = await apiFetch(`${API_URL}/dial/${lead.id}`, { method: "POST" });
      const data = await res.json();
      if (!res.ok) {
        const msg = data.error || `Dial failed (HTTP ${res.status})`;
        if (res.status === 402) {
          showBillingPrompt(msg);
        } else {
          alert(msg);
        }
      } else {
        alert(`Status: ${data.message || 'Connecting call...'}`);
      }
    } catch { alert("Failed to hit the dialer API. Check console.");
     }
    setTimeout(() => setDialingId(null), 10000);
  }, [apiFetch, showBillingPrompt]);

  const handleWebCall = useCallback(async (lead) => {
    if (webCallActive === lead.id) {
      // Disconnect active simulation
      if (webCallWsRef.current) {
        const ws = webCallWsRef.current;
        try {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ event: 'stop' }));
          } else {
            ws.close();
          }
        } catch { /* ignore */ }
        setTimeout(() => {
          try {
            if (ws.readyState !== WebSocket.CLOSED) ws.close();
          } catch { /* ignore */ }
        }, 500);
      }
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({
          audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }
        });
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
        ws.send(JSON.stringify({ event: 'start', start: { stream_sid: sid, user_email: currentUser?.email || '' }, stream_sid: sid, user_email: currentUser?.email || '' }));

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
        let activeSources = [];
        ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          if (data.type === 'clear') {
            // Backend barge-in — stop all queued audio immediately
            console.log('[barge-in] clear received, stopping', activeSources.length, 'sources');
            activeSources.forEach(s => { try { s.stop(); } catch { /* ignore */ } });
            activeSources = [];
            nextPlayTime = audioContext.currentTime;
            if (unmuteTimer) { clearTimeout(unmuteTimer); unmuteTimer = null; }
            micMuted = false;
          } else if (data.event === 'media') {
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
            activeSources.push(destSource);
            destSource.onended = () => { activeSources = activeSources.filter(s => s !== destSource); };

            // Unmute mic once, 400ms after the first chunk — don't reset on every chunk
            if (micMuted && !unmuteTimer) {
              unmuteTimer = setTimeout(() => { micMuted = false; unmuteTimer = null; }, 400);
            }
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
              formData.append('stream_sid', sid);
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
  }, [apiFetch, webCallActive, orgProducts, activeVoiceProvider, activeVoiceId, activeLanguage]);

  const getBrowserAccountId = useCallback((campaignId) => {
    if (!campaignId) return 0;
    try {
      const raw = localStorage.getItem(`callified_browser_account_campaign_${campaignId}`);
      const id = raw ? parseInt(raw, 10) : 0;
      return isNaN(id) ? 0 : id;
    } catch {
      return 0;
    }
  }, []);

  const handleCampaignDial = useCallback(async (lead, campaignId, exotelAccountId) => {
    setDialingId(lead.id);
    try {
      const accountId = exotelAccountId && !isNaN(exotelAccountId) ? parseInt(exotelAccountId, 10) : getBrowserAccountId(campaignId);
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/dial/${lead.id}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ exotel_account_id: accountId || 0 }),
      });
      if (!res.ok) {
        // Surface the backend error so silent failures, especially the
        // minute-balance gate, don't look like nothing happened.
        const body = await res.json().catch(() => ({}));
        const msg = body.error || `Dial failed (HTTP ${res.status})`;
        if (res.status === 402) {
          // Show the themed billing modal instead
          // of native confirm() (which renders in the OS theme and clashes).
          showBillingPrompt(msg);
        } else if (/dnd/i.test(msg)) {
          // DND blocks already render an inline "🚫 DND — number blocked"
          // badge on the row + a transient flash from handleDialClick.
          // The native alert() here was duplicate noise — drop it silently.
        } else {
          alert(msg);
        }
      }
    } catch(e) {
      alert('Network error: ' + (e?.message || 'unknown'));
    }
    setTimeout(() => setDialingId(null), 10000);
  }, [apiFetch, getBrowserAccountId, showBillingPrompt]);

  const ensureMicrophoneAvailable = useCallback(async () => {
    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error('Microphone is not available in this browser');
    }
    let stream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
    } catch (e) {
      const name = e?.name || '';
      if (name === 'NotAllowedError' || name === 'PermissionDeniedError') {
        throw new Error('Microphone access denied. Please allow microphone access before calling.');
      }
      if (name === 'NotFoundError' || name === 'DevicesNotFoundError') {
        throw new Error('No microphone found. Connect or enable a microphone before calling.');
      }
      throw new Error(`Microphone check failed: ${e?.message || 'unknown error'}`);
    } finally {
      if (stream) stream.getTracks().forEach(track => track.stop());
    }
  }, []);

  const triggerBrowserCall = useCallback(async (lead, campaignId, onEnded, exotelAccountId, scheduledCallId) => {
    if (!lead || !campaignId) return false;
    const accountId = exotelAccountId && !isNaN(exotelAccountId) ? parseInt(exotelAccountId, 10) : getBrowserAccountId(campaignId);
    // Scheduled callbacks may rely on the campaign's default provider account,
    // so only require an explicit account for manual browser calls.
    if (!accountId && !scheduledCallId) {
      toast({ message: 'Select a browser call account before calling', kind: 'error' });
      return false;
    }
    try {
      await ensureMicrophoneAvailable();
    } catch (e) {
      toast({ message: e?.message || 'Microphone is required before calling', kind: 'error' });
      return false;
    }
    browserCallEndedCbRef.current = onEnded || null;
    setBrowserCallLead(lead);
    setBrowserCallCampaignId(campaignId);
    setBrowserCallSid(null);
    setBrowserCallDialing(true);
    try {
      const res = await apiFetch(`${API_URL}/campaigns/${campaignId}/leads/${lead.id}/browser-call`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          exotel_account_id: accountId || 0,
          scheduled_call_id: scheduledCallId || 0,
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        const msg = data.error || `Browser call failed (HTTP ${res.status})`;
        if (res.status === 402) {
          showBillingPrompt(msg);
          return false;
        }
        throw new Error(msg);
      }
      setBrowserCallSid(data.call_sid || data.sid);
      return true;
    } catch (e) {
      toast({ message: e?.message || 'Browser call failed', kind: 'error' });
      setBrowserCallLead(null);
      setBrowserCallCampaignId(null);
      setBrowserCallSid(null);
      browserCallEndedCbRef.current = null;
      return false;
    } finally {
      setBrowserCallDialing(false);
    }
  }, [apiFetch, toast, getBrowserAccountId, ensureMicrophoneAvailable, showBillingPrompt]);

  const closeBrowserCall = useCallback(() => {
    browserCallEndedCbRef.current = null;
    setBrowserCallLead(null);
    setBrowserCallCampaignId(null);
    setBrowserCallSid(null);
  }, []);

  const startScheduledCallback = useCallback(() => {
    const call = scheduledCallbackPreview;
    if (!call) return;
    setScheduledCallbackPreview(null);
    const lead = {
      id: call.lead_id,
      first_name: call.first_name || 'Customer',
      phone: call.phone || '',
    };
    triggerBrowserCall(lead, call.campaign_id, undefined, undefined, call.id);
  }, [scheduledCallbackPreview, triggerBrowserCall]);

  const dismissScheduledCallbackPreview = useCallback(() => {
    const call = scheduledCallbackPreview;
    if (call) {
      dismissScheduledCall(call.id);
    }
    setScheduledCallbackPreview(null);
  }, [scheduledCallbackPreview, dismissScheduledCall]);

  // Poll for due manual scheduled calls. Calls scheduled by the current user
  // open a preview modal with customer details, remarks, and call history;
  // the agent must click Start Call to connect. Everyone else just sees a reminder.
  const fetchDueManualCalls = useCallback(async () => {
    try {
      const res = await apiFetch(`${API_URL}/scheduled-calls?mode=manual&status=pending&due=true&lead_time_seconds=10`);
      if (!res.ok) return;
      const calls = await res.json();
      setDueManualCalls(calls || []);

      const myUserId = currentUser?.id;
      if (!myUserId) return;
      // Only show one preview at a time and don't interrupt an active call.
      if (scheduledCallbackPreview || browserCallLead || browserCallDialing) return;
      // Respect client-side dismissal so a dismissed callback does not
      // reappear after a refresh while the backend row is still pending.
      const visibleCalls = (calls || []).filter(c => !dismissedIds.has(c.id));
      for (const call of visibleCalls) {
        if (call.scheduled_by_user_id !== myUserId) continue;
        if (triggeredScheduledRef.current.has(call.id)) continue;
        if (!call.campaign_id || !call.lead_id) continue;
        triggeredScheduledRef.current.add(call.id);
        setScheduledCallbackPreview(call);
        break;
      }
    } catch (e) {
      console.error('[scheduled-calls] poll failed', e);
    }
  }, [apiFetch, currentUser?.id, scheduledCallbackPreview, browserCallLead, browserCallDialing, dismissedIds]);

  const handleRescheduled = useCallback((callId) => {
    fetchDueManualCalls();
    if (callId) clearDismissedScheduledCall(callId);
  }, [fetchDueManualCalls, clearDismissedScheduledCall]);

  const handleBrowserCallEnded = useCallback((status, errorMsg) => {
    const cb = browserCallEndedCbRef.current;
    browserCallEndedCbRef.current = null;
    if (cb) {
      cb(status, errorMsg);
      // When an ended-callback is registered (browser auto-dial), close the
      // call modal automatically so the disposition screen is visible.
      setBrowserCallLead(null);
      setBrowserCallCampaignId(null);
      setBrowserCallSid(null);
    }
  }, []);

  const handleCampaignWebCall = useCallback(async (lead, campaignId) => {
    if (webCallActive === lead.id) {
      if (webCallWsRef.current) {
        const ws = webCallWsRef.current;
        try {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ event: 'stop' }));
          } else {
            ws.close();
          }
        } catch { /* ignore */ }
        setTimeout(() => {
          try {
            if (ws.readyState !== WebSocket.CLOSED) ws.close();
          } catch { /* ignore */ }
        }, 500);
      }
      return;
    }
    // Fetch campaign voice settings before starting call
    let campVoice = {};
    try {
      const vRes = await apiFetch(`${API_URL}/campaigns/${campaignId}/voice-settings`);
      campVoice = await vRes.json();
    } catch { /* ignore */ }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({
          audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }
        });
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
        tts_provider: campVoice.tts_provider || activeVoiceProvider,
        voice: campVoice.tts_voice_id || activeVoiceId,
        tts_language: campVoice.tts_language || activeLanguage,
        max_call_duration_seconds: String(campVoice.max_call_duration_seconds || 0),
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
        let activeSources = [];
        ws.onmessage = (event) => {
          const data = JSON.parse(event.data);
          if (data.type === 'clear') {
            // Backend barge-in — stop all queued audio immediately
            console.log('[barge-in] clear received, stopping', activeSources.length, 'sources');
            activeSources.forEach(s => { try { s.stop(); } catch { /* ignore */ } });
            activeSources = [];
            nextPlayTime = audioContext.currentTime;
            if (unmuteTimer) { clearTimeout(unmuteTimer); unmuteTimer = null; }
            micMuted = false;
          } else if (data.event === 'media') {
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
            activeSources.push(destSource);
            destSource.onended = () => { activeSources = activeSources.filter(s => s !== destSource); };

            // Unmute mic once, 400ms after the first chunk — don't reset on every chunk
            if (micMuted && !unmuteTimer) {
              unmuteTimer = setTimeout(() => { micMuted = false; unmuteTimer = null; }, 400);
            }
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
              formData.append('campaign_id', String(campaignId));
              formData.append('stream_sid', sid);
              try {
                const res = await apiFetch(`${API_URL}/upload-recording`, { method: 'POST', body: formData });
                if (!res.ok) console.error(`[RECORDING] Upload failed: HTTP ${res.status}`);
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
  }, [apiFetch, webCallActive, orgProducts, activeVoiceProvider, activeVoiceId, activeLanguage]);

  useEffect(() => {
    if (!currentUser?.id) return;
    fetchDueManualCalls();
    const id = setInterval(fetchDueManualCalls, 5000);
    return () => clearInterval(id);
  }, [currentUser?.id, fetchDueManualCalls]);

  // Agent presence heartbeat: on_call when in a browser/sim call, otherwise
  // respect the manual status (break/idle).
  useEffect(() => {
    if (!currentUser?.id) return;
    const sendHeartbeat = () => {
      let status = manualPresenceStatus;
      if (browserCallLead || webCallActive) {
        status = 'on_call';
      }
      apiFetch(`${API_URL}/presence/heartbeat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      }).catch(() => {});
    };
    sendHeartbeat();
    const id = setInterval(sendHeartbeat, 15000);
    return () => clearInterval(id);
  }, [currentUser?.id, manualPresenceStatus, browserCallLead, webCallActive, apiFetch]);

  return (
    <CallContext.Provider value={{
      dialingId, setDialingId,
      webCallActive, setWebCallActive,
      handleDial, handleWebCall,
      handleCampaignDial, handleCampaignWebCall,
      browserCallLead, browserCallDialing,
      triggerBrowserCall, closeBrowserCall,
      refreshScheduledCalls: fetchDueManualCalls,
      clearDismissedScheduledCall,
      dismissScheduledCall,
      dueScheduledCalls,
      manualPresenceStatus,
      setManualPresenceStatus,
    }}>
      {children}
      {browserCallLead && browserCallSid && (
        <BrowserCallModal
          lead={browserCallLead}
          callSid={browserCallSid}
          wsBaseUrl={(window.location.protocol === 'https:' ? 'wss:' : 'ws:') + '//' + window.location.host}
          onClose={closeBrowserCall}
          onEnded={handleBrowserCallEnded}
        />
      )}

      {scheduledCallbackPreview && (
        <ScheduledCallbackPreview
          call={scheduledCallbackPreview}
          onStart={startScheduledCallback}
          onDismiss={dismissScheduledCallbackPreview}
          onRescheduled={handleRescheduled}
          apiFetch={apiFetch}
          API_URL={API_URL}
          currentUser={currentUser}
          toast={toast}
        />
      )}

      {rechargePrompt && (
        <div onClick={() => setRechargePrompt(null)} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }} style={{
          position: 'fixed', inset: 0, background: 'rgba(17,24,39,0.45)',
          backdropFilter: 'blur(4px)', display: 'flex', alignItems: 'center',
          justifyContent: 'center', zIndex: 10000, padding: '1rem'
        }}>
          <div onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }} style={{
            maxWidth: '440px', width: '100%', padding: '1.75rem',
            background: '#ffffff',
            border: '1px solid rgba(99,102,241,0.28)',
            borderTop: '4px solid #6366f1',
            borderRadius: '12px',
            boxShadow: '0 24px 48px rgba(15,23,42,0.18)',
            color: '#111827',
          }}>
            <div style={{display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '20px'}}>
              <div style={{
                width: '40px', height: '40px', borderRadius: '50%',
                background: 'rgba(99,102,241,0.10)', border: '2px solid rgba(99,102,241,0.35)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: '1.2rem',
              }}>⚠️</div>
              <div>
                <h3 style={{margin: 0, fontSize: '1.05rem', fontWeight: 700, color: '#111827'}}>Recharge Required</h3>
                <div style={{fontSize: '0.75rem', color: '#6b7280', marginTop: '2px'}}>Outbound calls are paused</div>
              </div>
            </div>
            <p style={{
              margin: '0 0 18px 0', fontSize: '0.9rem', lineHeight: 1.55,
              color: '#374151', padding: '12px 14px', background: '#f8fafc',
              border: '1px solid #e5e7eb', borderRadius: '8px',
            }}>
              {rechargePrompt}
            </p>
            <p style={{
              margin: '0 0 20px 0', fontSize: '0.85rem', color: '#6b7280',
            }}>
              Open <strong style={{color: '#6366f1'}}>Billing</strong> to add call credits and continue dialing.
            </p>
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={() => setRechargePrompt(null)} style={{
                padding: '8px 16px', borderRadius: '8px', cursor: 'pointer',
                background: '#ffffff',
                border: '1px solid #d1d5db',
                color: '#374151', fontSize: '0.85rem', fontWeight: 600,
              }}>Cancel</button>
              <button onClick={() => { setRechargePrompt(null); window.location.assign('/billing'); }} style={{
                padding: '8px 18px', borderRadius: '8px', cursor: 'pointer',
                background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
                border: '1px solid rgba(99,102,241,0.55)', color: '#fff', fontSize: '0.85rem', fontWeight: 700,
                boxShadow: '0 6px 16px rgba(99,102,241,0.25)',
              }}>Open Billing →</button>
            </div>
          </div>
        </div>
      )}

      {minuteBalancePrompt && (
        <div onClick={() => setMinuteBalancePrompt(null)} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }} style={{
          position: 'fixed', inset: 0, background: 'rgba(17,24,39,0.45)',
          backdropFilter: 'blur(4px)', display: 'flex', alignItems: 'center',
          justifyContent: 'center', zIndex: 10000, padding: '1rem'
        }}>
          <div onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }} style={{
            maxWidth: '400px', width: '100%', padding: '1.75rem',
            background: '#ffffff',
            border: '1px solid rgba(99,102,241,0.28)',
            borderTop: '4px solid #6366f1',
            borderRadius: '12px',
            boxShadow: '0 24px 48px rgba(15,23,42,0.18)',
            color: '#111827',
          }}>
            <div style={{display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '14px'}}>
              <div style={{
                width: '40px', height: '40px', borderRadius: '50%',
                background: 'rgba(99,102,241,0.10)', border: '2px solid rgba(99,102,241,0.35)',
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                fontSize: '1.2rem',
              }}>⚠️</div>
              <div>
                <div style={{fontSize: '1.05rem', color: '#111827', fontWeight: 700}}>{minuteBalancePrompt}</div>
              </div>
            </div>
            <p style={{
              margin: '0 0 20px 0', fontSize: '0.9rem', lineHeight: 1.55,
              color: '#374151', padding: '12px 14px', background: '#f8fafc',
              border: '1px solid #e5e7eb', borderRadius: '8px',
            }}>
              Please recharge to continue
            </p>
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={() => setMinuteBalancePrompt(null)} style={{
                padding: '8px 18px', borderRadius: '8px', cursor: 'pointer',
                background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
                border: '1px solid rgba(99,102,241,0.55)', color: '#fff', fontSize: '0.85rem', fontWeight: 700,
                boxShadow: '0 6px 16px rgba(99,102,241,0.25)',
              }}>OK</button>
            </div>
          </div>
        </div>
      )}
    </CallContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useCall() {
  const ctx = useContext(CallContext);
  if (!ctx) throw new Error('useCall must be used within CallProvider');
  return ctx;
}
