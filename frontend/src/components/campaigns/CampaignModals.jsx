import React, { useState } from 'react';
import { CAMPAIGN_TEMPLATES, INDUSTRY_COLORS, LANGUAGE_LABELS } from '../../constants/campaignTemplates';
import { validateCampaignName, CAMPAIGN_NAME_MAX_LEN } from '../../utils/campaignName';

export default function CampaignModals({
  // Create Campaign Modal
  showCreateModal, setShowCreateModal,
  createForm, setCreateForm,
  handleCreateCampaign, loading, createError, orgProducts,
  selectedTemplate, setSelectedTemplate,
  // Add Leads Modal
  showAddLeadsModal, setShowAddLeadsModal,
  availableLeads, selectedLeadIds, toggleLeadSelection,
  handleAddLeads,
  // CSV Import Modal
  showCsvImportModal, setShowCsvImportModal,
  csvFile, setCsvFile, handleCsvImport,
  // Edit Lead Modal
  editLead, setEditLead,
  editForm, setEditForm, handleSaveEdit,
  // Edit Campaign Modal
  showEditCampaignModal, setShowEditCampaignModal,
  editCampaignForm, setEditCampaignForm,
  handleSaveEditCampaign,
}) {
  const [nameTouched, setNameTouched] = useState(false);
  const nameError = validateCampaignName(createForm.name);
  const showNameError = nameTouched && !!nameError;

  // Render-time dedupe: when the products table has duplicate (org_id, name)
  // rows from before the API uniqueness check landed, the dropdown shows
  // "EmpMonitor / EmpMonitor" with two ids. Hide the higher-id dups here so
  // the dropdown is clean. The full list is kept in orgProducts so id-based
  // lookups (campaign header badge etc.) still resolve.
  const dedupedProducts = (() => {
    const seen = new Map();
    (orgProducts || []).forEach(p => {
      const key = (p?.name || '').trim().toLowerCase();
      if (!key) return;
      const existing = seen.get(key);
      if (!existing || p.id < existing.id) seen.set(key, p);
    });
    return Array.from(seen.values()).sort((a, b) => b.id - a.id);
  })();

  const handleClose = () => {
    setNameTouched(false);
    setShowCreateModal(false);
    if (setSelectedTemplate) setSelectedTemplate(null);
  };

  return (
    <>
      {/* Create Campaign Modal */}
      {showCreateModal && (
        <div className="modal-overlay" onClick={handleClose}>
          <div className="glass-panel" onClick={e => e.stopPropagation()}
            style={{maxWidth: '680px', width: '95%', maxHeight: '85vh', overflowY: 'auto'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Create New Campaign</h3>

            {/* Template selector */}
            <div style={{marginBottom: '1.5rem'}}>
              <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '8px'}}>Start from a template (optional)</label>
              <div style={{display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(190px, 1fr))', gap: '8px'}}>
                {CAMPAIGN_TEMPLATES.map(tpl => {
                  const ic = INDUSTRY_COLORS[tpl.industry] || { bg: 'rgba(148,163,184,0.15)', color: '#94a3b8' };
                  const isSelected = selectedTemplate?.id === tpl.id;
                  return (
                    <div key={tpl.id}
                      onClick={() => {
                        if (isSelected) {
                          setSelectedTemplate(null);
                          setCreateForm(f => ({...f, name: ''}));
                        } else {
                          setSelectedTemplate(tpl);
                          setCreateForm(f => ({...f, name: tpl.name}));
                        }
                      }}
                      style={{
                        padding: '10px 12px', borderRadius: '8px', cursor: 'pointer',
                        border: isSelected ? '2px solid #60a5fa' : '1px solid rgba(255,255,255,0.08)',
                        background: isSelected ? 'rgba(96,165,250,0.1)' : 'rgba(255,255,255,0.03)',
                        transition: 'all 0.15s ease',
                      }}>
                      <div style={{fontWeight: 600, fontSize: '0.82rem', color: '#e2e8f0', marginBottom: '4px'}}>{tpl.name}</div>
                      <div style={{display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '4px'}}>
                        <span style={{background: ic.bg, color: ic.color, fontSize: '0.65rem', padding: '1px 6px', borderRadius: '8px', fontWeight: 600}}>
                          {tpl.industry}
                        </span>
                        <span style={{background: 'rgba(148,163,184,0.15)', color: '#94a3b8', fontSize: '0.65rem', padding: '1px 6px', borderRadius: '8px', fontWeight: 600}}>
                          {LANGUAGE_LABELS[tpl.language] || tpl.language}
                        </span>
                      </div>
                      <div style={{fontSize: '0.72rem', color: '#64748b', lineHeight: '1.3'}}>{tpl.description}</div>
                    </div>
                  );
                })}
              </div>
              {selectedTemplate && (
                <div style={{marginTop: '8px', padding: '8px 12px', borderRadius: '6px', background: 'rgba(96,165,250,0.08)', border: '1px solid rgba(96,165,250,0.2)'}}>
                  <div style={{fontSize: '0.75rem', color: '#60a5fa', fontWeight: 600, marginBottom: '2px'}}>
                    Template selected: {selectedTemplate.name}
                  </div>
                  <div style={{fontSize: '0.7rem', color: '#94a3b8'}}>
                    Voice: {selectedTemplate.tts_provider} / {selectedTemplate.tts_voice_id} &middot; Will auto-set voice settings{createForm.product_id ? ' and product prompt' : ' (select a product to also set the prompt)'}
                  </div>
                </div>
              )}
            </div>

            <div style={{borderTop: '1px solid rgba(255,255,255,0.06)', paddingTop: '1rem'}}>
              <form onSubmit={e => {
                setNameTouched(true);
                if (nameError) { e.preventDefault(); return; }
                handleCreateCampaign(e);
              }}>
                <div style={{marginBottom: '1rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                    Campaign Name <span style={{color: '#ef4444'}}>*</span>
                  </label>
                  <input
                    className="form-input"
                    placeholder="e.g. AdsGPT March Campaign"
                    value={createForm.name}
                    maxLength={CAMPAIGN_NAME_MAX_LEN}
                    onChange={e => { setNameTouched(true); setCreateForm({...createForm, name: e.target.value}); }}
                    onBlur={() => setNameTouched(true)}
                    style={{
                      width: '100%',
                      borderColor: showNameError ? 'rgba(239,68,68,0.6)' : undefined,
                      boxShadow: showNameError ? '0 0 0 3px rgba(239,68,68,0.15)' : undefined,
                    }}
                  />
                  {showNameError && (
                    <p style={{margin: '4px 0 0', fontSize: '0.78rem', color: '#f87171'}}>
                      {nameError}
                    </p>
                  )}
                </div>
                <div style={{marginBottom: '1.5rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                    Product {selectedTemplate && <span style={{color: '#60a5fa', fontSize: '0.75rem'}}>(required to apply prompt template)</span>}
                  </label>
                  <select className="form-input" value={createForm.product_id}
                    onChange={e => setCreateForm({...createForm, product_id: e.target.value})}
                    style={{width: '100%'}}>
                    <option value="">-- Select Product --</option>
                    {dedupedProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                </div>
                <div style={{marginBottom: '1.5rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Lead Source (where did these leads come from?)</label>
                  <select className="form-input" value={createForm.lead_source}
                    onChange={e => setCreateForm({...createForm, lead_source: e.target.value})}
                    style={{width: '100%'}}>
                    <option value="">-- Select Source --</option>
                    <option value="facebook">Facebook / Meta Ads</option>
                    <option value="google">Google Ads</option>
                    <option value="instagram">Instagram</option>
                    <option value="linkedin">LinkedIn</option>
                    <option value="website">Website Form</option>
                    <option value="referral">Referral</option>
                    <option value="cold">Cold Outreach</option>
                  </select>
                </div>
                <div style={{marginBottom: '1.5rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Communication Channel</label>
                  <select className="form-input" value={createForm.channel || 'voice'}
                    onChange={e => setCreateForm({...createForm, channel: e.target.value})}
                    style={{width: '100%'}}>
                    <option value="voice">📞 Voice Call (AI Phone)</option>
                    <option value="whatsapp">💬 WhatsApp (AI Chat)</option>
                  </select>
                </div>
                {createError && (
                  <div style={{marginBottom: '1rem', padding: '10px 14px', borderRadius: '8px',
                    background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)',
                    color: '#fca5a5', fontSize: '0.85rem'}}>
                    {createError}
                  </div>
                )}
                <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                  <button type="button" onClick={handleClose}
                    style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                    Cancel
                  </button>
                  <button type="submit" className="btn-primary" disabled={loading || !!nameError}>
                    {loading ? 'Creating...' : selectedTemplate ? 'Create from Template' : 'Create'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        </div>
      )}

      {/* Add Leads Modal */}
      {showAddLeadsModal && (
        <div className="modal-overlay" onClick={() => setShowAddLeadsModal(false)}>
          <div className="glass-panel" onClick={e => e.stopPropagation()}
            style={{maxWidth: '500px', width: '90%', maxHeight: '70vh', display: 'flex', flexDirection: 'column'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Add Leads to Campaign</h3>
            {availableLeads.length === 0 ? (
              <p style={{color: '#64748b'}}>All leads are already in this campaign.</p>
            ) : (
              <div style={{flex: 1, overflowY: 'auto', marginBottom: '1rem'}}>
                {availableLeads.map(lead => (
                  <label key={lead.id} style={{display: 'flex', alignItems: 'center', gap: '10px', padding: '8px 4px', cursor: 'pointer', borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                    <input type="checkbox" checked={selectedLeadIds.includes(lead.id)}
                      onChange={() => toggleLeadSelection(lead.id)} />
                    <span style={{color: '#e2e8f0', fontWeight: 500}}>{lead.first_name} {lead.last_name}</span>
                    <span style={{color: '#64748b', fontSize: '0.8rem'}}>{lead.phone}</span>
                  </label>
                ))}
              </div>
            )}
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={() => setShowAddLeadsModal(false)}
                style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                Cancel
              </button>
              <button className="btn-primary" onClick={handleAddLeads} disabled={loading || selectedLeadIds.length === 0}>
                {loading ? 'Adding...' : `Add Selected (${selectedLeadIds.length})`}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* CSV Import Modal */}
      {showCsvImportModal && (
        <div className="modal-overlay" onClick={() => setShowCsvImportModal(false)}>
          <div className="glass-panel" onClick={e => e.stopPropagation()}
            style={{maxWidth: '450px', width: '90%'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Import Leads from CSV</h3>
            <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '1rem'}}>
              Upload a CSV with columns: first_name, last_name, phone, source. Leads will be created and added to this campaign.
            </p>
            <input type="file" accept=".csv" onChange={e => setCsvFile(e.target.files[0])}
              style={{marginBottom: '1rem', color: '#e2e8f0', fontSize: '0.85rem'}} />
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={() => setShowCsvImportModal(false)}
                style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                Cancel
              </button>
              <button className="btn-primary" onClick={handleCsvImport} disabled={loading || !csvFile}>
                {loading ? 'Importing...' : 'Import & Add to Campaign'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Edit Campaign Modal */}
      {showEditCampaignModal && (
        <div className="modal-overlay" onClick={() => setShowEditCampaignModal(false)}>
          <div className="glass-panel" onClick={e => e.stopPropagation()}
            style={{maxWidth: '450px', width: '90%'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Edit Campaign</h3>
            {(() => {
              const editNameError = validateCampaignName(editCampaignForm.name);
              return (
            <form onSubmit={e => {
              if (editNameError) { e.preventDefault(); return; }
              handleSaveEditCampaign(e);
            }}>
              <div style={{marginBottom: '1rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Campaign Name</label>
                <input className="form-input" placeholder="e.g. AdsGPT March Campaign"
                  value={editCampaignForm.name}
                  maxLength={CAMPAIGN_NAME_MAX_LEN}
                  onChange={e => setEditCampaignForm({...editCampaignForm, name: e.target.value})}
                  style={{
                    width: '100%',
                    borderColor: editNameError ? 'rgba(239,68,68,0.6)' : undefined,
                    boxShadow: editNameError ? '0 0 0 3px rgba(239,68,68,0.15)' : undefined,
                  }} />
                {editNameError && (
                  <p style={{margin: '4px 0 0', fontSize: '0.78rem', color: '#f87171'}}>{editNameError}</p>
                )}
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Product</label>
                <select className="form-input" value={editCampaignForm.product_id}
                  onChange={e => setEditCampaignForm({...editCampaignForm, product_id: e.target.value})}
                  style={{width: '100%'}}>
                  <option value="">-- Select Product --</option>
                  {dedupedProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Lead Source</label>
                <select className="form-input" value={editCampaignForm.lead_source}
                  onChange={e => setEditCampaignForm({...editCampaignForm, lead_source: e.target.value})}
                  style={{width: '100%'}}>
                  <option value="">-- Select Source --</option>
                  <option value="facebook">Facebook / Meta Ads</option>
                  <option value="google">Google Ads</option>
                  <option value="instagram">Instagram</option>
                  <option value="linkedin">LinkedIn</option>
                  <option value="website">Website Form</option>
                  <option value="referral">Referral</option>
                  <option value="cold">Cold Outreach</option>
                </select>
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>Communication Channel</label>
                <select className="form-input" value={editCampaignForm.channel || 'voice'}
                  onChange={e => setEditCampaignForm({...editCampaignForm, channel: e.target.value})}
                  style={{width: '100%'}}>
                  <option value="voice">📞 Voice Call (AI Phone)</option>
                  <option value="whatsapp">💬 WhatsApp (AI Chat)</option>
                </select>
              </div>
              <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                <button type="button" onClick={() => setShowEditCampaignModal(false)}
                  style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                  Cancel
                </button>
                <button type="submit" className="btn-primary" disabled={loading || !!editNameError}>
                  {loading ? 'Saving...' : 'Save Changes'}
                </button>
              </div>
            </form>
              );
            })()}
          </div>
        </div>
      )}

      {/* Edit Lead Modal */}
      {editLead && (
        <div className="modal-overlay" onClick={() => setEditLead(null)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()} style={{maxWidth: '420px'}}>
            <h2 style={{marginTop: 0, marginBottom: '1.5rem'}}>Edit Lead</h2>
            <div className="form-group">
              <label>First Name</label>
              <input className="form-input" value={editForm.first_name} onChange={e => setEditForm({...editForm, first_name: e.target.value})} />
            </div>
            <div className="form-group">
              <label>Last Name</label>
              <input className="form-input" value={editForm.last_name} onChange={e => setEditForm({...editForm, last_name: e.target.value})} />
            </div>
            <div className="form-group">
              <label>Phone (10 digits)</label>
              <input className="form-input" value={editForm.phone}
                inputMode="numeric" maxLength={10} pattern="\d{10}"
                onChange={e => setEditForm({...editForm, phone: e.target.value.replace(/\D/g, '').slice(0, 10)})} />
            </div>
            <div className="form-group">
              <label>Source</label>
              <input className="form-input" value={editForm.source} onChange={e => setEditForm({...editForm, source: e.target.value})} />
            </div>
            <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '1.5rem'}}>
              <button onClick={() => setEditLead(null)} style={{background: 'transparent', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', padding: '8px 18px', borderRadius: '8px', cursor: 'pointer'}}>Cancel</button>
              <button className="btn-primary" onClick={() => {
                if (!/^\d{10}$/.test(editForm.phone || '')) { alert('Phone must be exactly 10 digits'); return; }
                handleSaveEdit();
              }}>Save</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
