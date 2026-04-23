import React from 'react';
import { formatDate } from '../../utils/dateFormat';

export default function SettingsTab({
  handleAddPronunciation, pronFormData, setPronFormData, pronunciations, handleDeletePronunciation,
  selectedOrg, orgs, showProductInput, setShowProductInput, newProductName, setNewProductName,
  handleAddProduct, orgProducts, handleDeleteProduct, handleSaveProduct, scraping, handleScrapeProduct,
  promptDirty, handleSaveSystemPrompt, promptSaving, systemPromptAuto, systemPromptCustom,
  setSystemPromptCustom, setPromptDirty,
  apiFetch, API_URL, orgTimezone
}) {
  const [productPrompts, setProductPrompts] = React.useState({});
  const loadedProductIds = React.useRef(new Set());

  // Load per-product prompts when products change
  React.useEffect(() => {
    if (!orgProducts || orgProducts.length === 0) return;
    orgProducts.forEach(p => {
      if (loadedProductIds.current.has(p.id)) return; // already loaded
      loadedProductIds.current.add(p.id);
      apiFetch(`${API_URL}/products/${p.id}/prompt`)
        .then(res => res.ok ? res.json() : { agent_persona: '', call_flow_instructions: '' })
        .then(data => {
          setProductPrompts(prev => ({
            ...prev,
            [p.id]: {
              ...(prev[p.id] || {}),
              agent_persona: data.agent_persona || '',
              call_flow_instructions: data.call_flow_instructions || '',
              expanded: prev[p.id]?.expanded || false,
              generating: prev[p.id]?.generating || false,
              saving: prev[p.id]?.saving || false
            }
          }));
        })
        .catch(() => {
          setProductPrompts(prev => ({
            ...prev,
            [p.id]: { ...(prev[p.id] || {}), agent_persona: '', call_flow_instructions: '', expanded: false, generating: false, saving: false }
          }));
        });
    });
  }, [orgProducts]);

  const updateProductPrompt = (productId, field, value) => {
    setProductPrompts(prev => ({
      ...prev,
      [productId]: { ...prev[productId], [field]: value }
    }));
  };

  const handleGenerateProductPrompt = async (productId) => {
    updateProductPrompt(productId, 'generating', true);
    try {
      const res = await apiFetch(`${API_URL}/products/${productId}/generate-prompt`, {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
          agent_persona: productPrompts[productId]?.agent_persona || '',
          call_flow: productPrompts[productId]?.call_flow_instructions || ''
        })
      });
      const data = await res.json();
      if (data.prompt) {
        setSystemPromptCustom(data.prompt);
        setPromptDirty(true);
        alert('System prompt generated from product persona! Review below and click Save.');
      } else {
        alert(data.message || 'Generation failed');
      }
    } catch(e) { alert('Failed to generate'); }
    updateProductPrompt(productId, 'generating', false);
  };

  const handleGeneratePersona = async (productId) => {
    updateProductPrompt(productId, 'generatingPersona', true);
    try {
      const res = await apiFetch(`${API_URL}/products/${productId}/generate-persona`, {
        method: 'POST', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({})
      });
      const data = await res.json();
      if (data.status === 'success') {
        updateProductPrompt(productId, 'agent_persona', data.agent_persona);
        updateProductPrompt(productId, 'call_flow_instructions', data.call_flow_instructions);
        // Auto-save immediately after generation
        await apiFetch(`${API_URL}/products/${productId}/prompt`, {
          method: 'PUT', headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({ agent_persona: data.agent_persona, call_flow_instructions: data.call_flow_instructions })
        });
      } else {
        alert(data.message || 'Generation failed');
      }
    } catch(e) { alert('Failed to generate persona'); }
    updateProductPrompt(productId, 'generatingPersona', false);
  };

  const handleSaveProductPrompt = async (productId) => {
    updateProductPrompt(productId, 'saving', true);
    try {
      const pp = productPrompts[productId];
      const res = await apiFetch(`${API_URL}/products/${productId}/prompt`, {
        method: 'PUT', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
          agent_persona: pp?.agent_persona || '',
          call_flow_instructions: pp?.call_flow_instructions || ''
        })
      });
      if (!res.ok) {
        const err = await res.text();
        throw new Error(err || `HTTP ${res.status}`);
      }
      alert('Persona & call flow saved!');
    } catch(e) { alert('Failed to save: ' + (e.message || e)); }
    updateProductPrompt(productId, 'saving', false);
  };

  return (
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
              data-testid="pron-word"
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
              data-testid="pron-phonetic"
            />
          </div>
          <button data-testid="add-rule-btn" type="submit" className="btn-primary" style={{height: '46px', padding: '0 20px', whiteSpace: 'nowrap'}}>
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
                  <td style={{color: '#94a3b8', fontSize: '0.85rem'}}>{formatDate(p.created_at, orgTimezone)}</td>
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
              <button data-testid="add-product-btn" className="btn-primary" style={{background: 'linear-gradient(135deg, #22d3ee, #06b6d4)', fontSize: '0.85rem', padding: '6px 14px'}}
                onClick={() => setShowProductInput(true)}>+ Add Product</button>
            ) : (
              <div style={{display: 'flex', gap: '8px', alignItems: 'center'}}>
                <input data-testid="product-name-input" className="form-input" autoFocus placeholder="Product name (e.g. AdsGPT)..."
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
              {orgProducts.map(p => {
                const pp = productPrompts[p.id] || { agent_persona: '', call_flow_instructions: '', expanded: false, generating: false, saving: false };
                return (
                <div key={p.id} style={{background: 'rgba(0,0,0,0.2)', borderRadius: '12px', padding: '1.25rem', border: '1px solid rgba(255,255,255,0.05)'}}>
                  <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem'}}>
                    <div style={{display: 'flex', alignItems: 'center', gap: '8px', flex: 1}}>
                      {pp.editingName ? (
                        <>
                          <input className="form-input" autoFocus defaultValue={p.name}
                            onBlur={e => {
                              if (e.target.value.trim() && e.target.value !== p.name) handleSaveProduct(p.id, { name: e.target.value.trim() });
                              updateProductPrompt(p.id, 'editingName', false);
                            }}
                            onKeyDown={e => { if (e.key === 'Enter') e.target.blur(); }}
                            style={{fontWeight: 700, fontSize: '1.05rem', color: '#e2e8f0', border: '1px solid rgba(34,211,238,0.4)', borderRadius: '6px', padding: '4px 8px', maxWidth: '400px'}} />
                          <button style={{background: 'transparent', border: 'none', color: '#94a3b8', cursor: 'pointer', fontSize: '0.8rem'}}
                            onClick={() => updateProductPrompt(p.id, 'editingName', false)}>Cancel</button>
                        </>
                      ) : (
                        <>
                          <span style={{fontWeight: 700, fontSize: '1.05rem', color: '#e2e8f0'}}>{p.name}</span>
                          <button style={{background: 'rgba(34,211,238,0.1)', border: '1px solid rgba(34,211,238,0.25)', color: '#22d3ee', padding: '3px 10px', borderRadius: '6px', cursor: 'pointer', fontSize: '0.75rem', fontWeight: 600}}
                            onClick={() => updateProductPrompt(p.id, 'editingName', true)}>✏️ Edit</button>
                        </>
                      )}
                    </div>
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

                  {/* Collapsible: All product details */}
                  <div style={{marginTop: '0.5rem'}}>
                    <button
                      onClick={() => updateProductPrompt(p.id, 'expanded', !pp.expanded)}
                      style={{
                        background: 'rgba(34,211,238,0.08)', border: '1px solid rgba(34,211,238,0.2)',
                        color: '#22d3ee', padding: '8px 14px', borderRadius: '8px', cursor: 'pointer',
                        fontSize: '0.85rem', fontWeight: 600, width: '100%', textAlign: 'left'
                      }}>
                      {pp.expanded ? '▾' : '▸'} Product Details, Persona & Call Flow
                      {p.scraped_info ? ' ✅' : ''}
                      {pp.agent_persona ? ' 🎭' : ''}
                    </button>

                    {pp.expanded && (
                      <div style={{marginTop: '12px', padding: '14px', background: 'rgba(0,0,0,0.15)', borderRadius: '8px', border: '1px solid rgba(34,211,238,0.1)'}}>

                        {/* AI-Extracted Info */}
                        {p.scraped_info && (
                          <div style={{marginBottom: '1.25rem'}}>
                            <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, color: '#22d3ee', fontSize: '0.85rem'}}>📄 AI-Extracted Info</label>
                            <textarea readOnly value={p.scraped_info}
                              style={{
                                width: '100%', boxSizing: 'border-box',
                                background: 'rgba(0,0,0,0.3)', padding: '12px', borderRadius: '8px',
                                border: '1px solid rgba(34,211,238,0.15)', whiteSpace: 'pre-wrap',
                                color: '#cbd5e1', fontSize: '0.85rem', lineHeight: 1.6,
                                height: '220px', resize: 'vertical', overflowY: 'scroll',
                                fontFamily: 'inherit', cursor: 'text',
                              }} />
                          </div>
                        )}

                        {/* Manual Notes */}
                        <div style={{marginBottom: '1.25rem'}}>
                          <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, fontSize: '0.85rem'}}>📝 Manual Notes</label>
                          <textarea className="form-input" rows={3} placeholder="Pricing, USPs, objection handling..."
                            defaultValue={p.manual_notes}
                            onBlur={e => handleSaveProduct(p.id, { manual_notes: e.target.value })}
                            style={{resize: 'vertical', minHeight: '70px', fontSize: '0.85rem'}} />
                        </div>

                        <div style={{borderTop: '1px solid rgba(255,255,255,0.06)', margin: '1rem 0', paddingTop: '1rem'}}>
                          {(p.scraped_info || p.manual_notes) && (
                            <div style={{marginBottom: '1rem'}}>
                              <button className="btn-primary"
                                style={{background: 'linear-gradient(135deg, #818cf8, #6366f1)', fontSize: '0.85rem', padding: '8px 16px', width: '100%'}}
                                disabled={pp.generatingPersona}
                                onClick={() => handleGeneratePersona(p.id)}>
                                {pp.generatingPersona ? '⏳ Generating from website info...' : '✨ Auto-Generate Persona & Call Flow from Website'}
                              </button>
                            </div>
                          )}

                          {/* Agent Persona */}
                          <div style={{marginBottom: '1rem'}}>
                            <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, fontSize: '0.85rem', color: '#a78bfa'}}>🎭 Agent Persona</label>
                            <textarea className="form-input" rows={4}
                              value={pp.agent_persona}
                              onChange={e => updateProductPrompt(p.id, 'agent_persona', e.target.value)}
                              placeholder="e.g. You are Meera, a professional sales agent..."
                              style={{resize: 'vertical', minHeight: '80px', fontSize: '0.85rem', lineHeight: 1.6}} />
                          </div>

                          {/* Call Flow */}
                          <div style={{marginBottom: '1rem'}}>
                            <label style={{display: 'block', marginBottom: '6px', fontWeight: 600, fontSize: '0.85rem', color: '#22d3ee'}}>📋 Call Flow Instructions</label>
                            <textarea className="form-input" rows={5}
                              value={pp.call_flow_instructions}
                              onChange={e => updateProductPrompt(p.id, 'call_flow_instructions', e.target.value)}
                              placeholder="e.g. Step 1: Greet. Step 2: Qualify..."
                              style={{resize: 'vertical', minHeight: '100px', fontSize: '0.85rem', lineHeight: 1.6}} />
                          </div>

                          <div style={{display: 'flex', gap: '10px'}}>
                            <button className="btn-primary"
                              style={{background: 'linear-gradient(135deg, #f59e0b, #d97706)', fontSize: '0.85rem', padding: '8px 16px'}}
                              disabled={pp.generating}
                              onClick={() => handleGenerateProductPrompt(p.id)}>
                              {pp.generating ? '⏳ Generating...' : '🤖 Generate Prompt'}
                            </button>
                            <button className="btn-primary"
                              style={{background: 'linear-gradient(135deg, #10b981, #059669)', fontSize: '0.85rem', padding: '8px 16px'}}
                              disabled={pp.saving}
                              onClick={() => handleSaveProductPrompt(p.id)}>
                              {pp.saving ? '⏳ Saving...' : '💾 Save Persona & Flow'}
                            </button>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                </div>
                );
              })}
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
  );
}
