import React, { createContext, useContext, useState, useRef, useCallback } from 'react';
import { API_URL } from '../constants/api';
import { useAuth } from './AuthContext';
import { useOrg } from './OrgContext';
import { useVoice } from './VoiceContext';

const CallContext = createContext(null);

export function CallProvider({ children }) {
  const { apiFetch } = useAuth();
  const { orgProducts } = useOrg();
  const { activeVoiceProvider, activeVoiceId, activeLanguage } = useVoice();

  const [dialingId, setDialingId] = useState(null);
  const [webCallActive, setWebCallActive] = useState(null);
  const webCallWsRef = useRef(null);
  const webCallAudioCtxRef = useRef(null);

  const handleDial = useCallback(async (lead) => {
    setDialingId(lead.id);
    try {
      const res = await apiFetch(`${API_URL}/dial/${lead.id}`, { method: "POST" });
      const data = await res.json();
      alert(`Status: ${data.message || 'Connecting call...'}`);
    } catch(e) {
      alert("Failed to hit the dialer API. Check console.");
    }
    setTimeout(() => setDialingId(null), 10000);
  }, [apiFetch]);

  const handleWebCall = useCallback(async (lead) => {
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
  }, [apiFetch, webCallActive, orgProducts, activeVoiceProvider, activeVoiceId, activeLanguage]);

  const handleCampaignDial = useCallback(async (lead, campaignId) => {
    setDialingId(lead.id);
    try {
      await apiFetch(`${API_URL}/campaigns/${campaignId}/dial/${lead.id}`, { method: "POST" });
    } catch(e) {}
    setTimeout(() => setDialingId(null), 10000);
  }, [apiFetch]);

  const handleCampaignWebCall = useCallback(async (lead, campaignId) => {
    if (webCallActive === lead.id) {
      if (webCallWsRef.current) webCallWsRef.current.close();
      if (webCallAudioCtxRef.current) webCallAudioCtxRef.current.close();
      setWebCallActive(null);
      return;
    }
    // Fetch campaign voice settings before starting call
    let campVoice = {};
    try {
      const vRes = await apiFetch(`${API_URL}/campaigns/${campaignId}/voice-settings`);
      campVoice = await vRes.json();
    } catch(e) {}

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
        tts_provider: campVoice.tts_provider || activeVoiceProvider,
        voice: campVoice.tts_voice_id || activeVoiceId,
        tts_language: campVoice.tts_language || activeLanguage,
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
  }, [apiFetch, webCallActive, orgProducts, activeVoiceProvider, activeVoiceId, activeLanguage]);

  return (
    <CallContext.Provider value={{
      dialingId, setDialingId,
      webCallActive, setWebCallActive,
      handleDial, handleWebCall,
      handleCampaignDial, handleCampaignWebCall
    }}>
      {children}
    </CallContext.Provider>
  );
}

export function useCall() {
  const ctx = useContext(CallContext);
  if (!ctx) throw new Error('useCall must be used within CallProvider');
  return ctx;
}
