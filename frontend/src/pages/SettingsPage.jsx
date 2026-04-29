import React, { useState, useEffect } from 'react';
import SettingsTab from '../components/tabs/SettingsTab';

export default function SettingsPage({ apiFetch, API_URL, selectedOrg, orgs, orgProducts, orgTimezone, fetchOrgProducts }) {
  // Pronunciation State
  const [pronunciations, setPronunciations] = useState([]);
  const [pronFormData, setPronFormData] = useState({ word: '', phonetic: '' });

  // Product Input State
  const [newProductName, setNewProductName] = useState('');
  const [showProductInput, setShowProductInput] = useState(false);
  const [scraping, setScraping] = useState(null);
  const [addingProduct, setAddingProduct] = useState(false);
  const [productAddError, setProductAddError] = useState('');

  // System Prompt State
  const [systemPromptAuto, setSystemPromptAuto] = useState('');
  const [systemPromptCustom, setSystemPromptCustom] = useState('');
  const [promptSaving, setPromptSaving] = useState(false);
  const [promptDirty, setPromptDirty] = useState(false);
  const [promptSaveStatus, setPromptSaveStatus] = useState(''); // '' | 'saved' | 'error'

  useEffect(() => {
    fetchPronunciations();
    if (selectedOrg) fetchSystemPrompt(selectedOrg.id);
  }, [selectedOrg]);

  const fetchPronunciations = async () => {
    try { const res = await apiFetch(`${API_URL}/pronunciation`); setPronunciations(await res.json()); } catch(e){}
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

  const handleAddPronunciation = async (e) => {
    e.preventDefault();
    const word = pronFormData.word.trim();
    const phonetic = pronFormData.phonetic.trim();
    if (!word || !phonetic) return;
    // Allow-list mirroring the backend (issue #81): block <, >, {, }, backticks,
    // and any non-allowed character so prompt-injection / HTML payloads can't
    // reach the LLM prompt or other rendering surfaces.
    const allowed = /^[A-Za-z0-9][A-Za-z0-9 .'\-]{0,49}$/;
    if (!allowed.test(word)) {
      alert('Written Word: only letters, digits, spaces, hyphens, apostrophes, and periods are allowed (max 50 chars).');
      return;
    }
    if (!allowed.test(phonetic)) {
      alert('How to Pronounce: only letters, digits, spaces, hyphens, apostrophes, and periods are allowed (max 50 chars).');
      return;
    }
    if (word.toLowerCase() === phonetic.toLowerCase()) {
      alert('"How to Pronounce" must differ from "Written Word" — otherwise the rule has no effect. Try spacing the letters (e.g. "B D R P L") or a phonetic spelling (e.g. "Bee-Dee-Are-Pee-El").');
      return;
    }
    try {
      const res = await apiFetch(`${API_URL}/pronunciation`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ word, phonetic })
      });
      if (!res.ok) {
        let msg = `Failed to add rule (HTTP ${res.status})`;
        try { const data = await res.json(); if (data?.error || data?.detail) msg = data.error || data.detail; } catch(_) {}
        alert(msg);
        return;
      }
      setPronFormData({ word: '', phonetic: '' });
      fetchPronunciations();
    } catch(e) { alert('Failed to add rule: ' + (e?.message || 'network error')); }
  };

  const handleDeletePronunciation = async (id) => {
    try {
      await apiFetch(`${API_URL}/pronunciation/${id}`, { method: 'DELETE' });
      fetchPronunciations();
    } catch(e) { console.error(e); }
  };

  const handleAddProduct = async () => {
    if (!selectedOrg || !newProductName.trim() || addingProduct) return;
    setAddingProduct(true);
    setProductAddError('');
    try {
      const res = await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/products`, {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ name: newProductName.trim() })
      });
      if (res.status === 409) {
        const data = await res.json().catch(() => ({}));
        setProductAddError(`"${data.existing_name || newProductName.trim()}" already exists`);
        await fetchOrgProducts(selectedOrg.id);
        return;
      }
      if (!res.ok) {
        setProductAddError('Could not add product. Please try again.');
        return;
      }
      setNewProductName(''); setShowProductInput(false);
      await fetchOrgProducts(selectedOrg.id);
    } finally {
      setAddingProduct(false);
    }
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
    if (selectedOrg) fetchSystemPrompt(selectedOrg.id);
  };

  const handleDeleteProduct = async (productId) => {
    if (!confirm('Delete this product?')) return;
    await apiFetch(`${API_URL}/products/${productId}`, { method: 'DELETE' });
    fetchOrgProducts(selectedOrg.id);
  };

  const handleSaveSystemPrompt = async () => {
    if (!selectedOrg) return;
    setPromptSaving(true);
    setPromptSaveStatus('');
    try {
      const res = await apiFetch(`${API_URL}/organizations/${selectedOrg.id}/system-prompt`, {
        method: 'PUT', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ custom_prompt: systemPromptCustom })
      });
      if (!res.ok) {
        setPromptSaveStatus('error');
      } else {
        setPromptSaveStatus('saved');
        setPromptDirty(false);
        setTimeout(() => setPromptSaveStatus(''), 2500);
      }
    } catch (e) {
      setPromptSaveStatus('error');
    }
    setPromptSaving(false);
  };

  return (
    <SettingsTab
      orgTimezone={orgTimezone}
      handleAddPronunciation={handleAddPronunciation} pronFormData={pronFormData}
      setPronFormData={setPronFormData} pronunciations={pronunciations}
      handleDeletePronunciation={handleDeletePronunciation} selectedOrg={selectedOrg}
      orgs={orgs} showProductInput={showProductInput} setShowProductInput={setShowProductInput}
      newProductName={newProductName} setNewProductName={setNewProductName}
      handleAddProduct={handleAddProduct} orgProducts={orgProducts}
      addingProduct={addingProduct} productAddError={productAddError}
      handleDeleteProduct={handleDeleteProduct} handleSaveProduct={handleSaveProduct}
      scraping={scraping} handleScrapeProduct={handleScrapeProduct}
      promptDirty={promptDirty} handleSaveSystemPrompt={handleSaveSystemPrompt}
      promptSaving={promptSaving} promptSaveStatus={promptSaveStatus}
      systemPromptAuto={systemPromptAuto}
      systemPromptCustom={systemPromptCustom} setSystemPromptCustom={setSystemPromptCustom}
      setPromptDirty={setPromptDirty}
      apiFetch={apiFetch} API_URL={API_URL}
    />
  );
}
