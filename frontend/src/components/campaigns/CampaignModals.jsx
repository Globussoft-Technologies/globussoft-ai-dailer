import React, { useRef, useState } from 'react';
import { CAMPAIGN_TEMPLATES, INDUSTRY_COLORS, LANGUAGE_LABELS } from '../../constants/campaignTemplates';
import { validateCampaignName, CAMPAIGN_NAME_MAX_LEN } from '../../utils/campaignName';
import { useHideAiFeatures } from '../../hooks/useHideAiFeatures';
import { isValidPhone, PHONE_VALIDATION_MESSAGE } from '../../utils/phone';
import { useToast } from '../../contexts/UIContext';

export default function CampaignModals({
  // Create Campaign Modal
  showCreateModal, setShowCreateModal,
  createForm, setCreateForm,
  handleCreateCampaign, loading, createError, orgProducts,
  selectedTemplate, setSelectedTemplate,
  orgExotelAccounts,
  executives,
  // Add Leads Modal
  showAddLeadsModal, setShowAddLeadsModal,
  addLeadsSearch, addLeadsResults, addLeadsLoading, searchAvailableLeads,
  selectedLeadIds, toggleLeadSelection,
  handleAddLeads,
  // CSV Import Modal
  showCsvImportModal, setShowCsvImportModal,
  csvFile, setCsvFile, handleCsvImport,
  csvImportResult, setCsvImportResult, closeCsvImportModal,
  // Edit Lead Modal
  editLead, setEditLead,
  editForm, setEditForm, handleSaveEdit,
  editErrors, setEditErrors,
  // Edit Campaign Modal
  showEditCampaignModal, setShowEditCampaignModal,
  editCampaignForm, setEditCampaignForm,
  handleSaveEditCampaign,
  editCampaignError, setEditCampaignError,
  setCreateError,
}) {
  const hideAiFeatures = useHideAiFeatures();
  const toast = useToast();
  const csvInputRef = useRef(null);
  const [nameTouched, setNameTouched] = useState(false);
  const [addLeadsError, setAddLeadsError] = useState('');
  const nameError = validateCampaignName(createForm.name);
  const showNameError = nameTouched && !!nameError;
  const showNameEmptyError = showNameError && nameError.includes('required');
  const showNameInvalidError = showNameError && !nameError.includes('required');

  const [editNameTouched, setEditNameTouched] = useState(false);
  const [showAllRejected, setShowAllRejected] = useState(false);
  const MAX_VISIBLE_REJECTED = 50;
  const editNameError = validateCampaignName(editCampaignForm?.name);
  const showEditNameError = editNameTouched && !!editNameError;

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
    if (setCreateForm) setCreateForm({ name: '', product_id: '', lead_source: '', channel: 'voice', exotel_account_id: '', executive_ids: [] });
    if (setCreateError) setCreateError('');
  };

  const downloadRejectedCSV = (rejected) => {
    if (!Array.isArray(rejected) || rejected.length === 0) return;
    const header = 'Row,First Name,Phone,Reason\n';
    const rows = rejected.map(r => [
      r.row ?? '',
      (r.first_name || '').replace(/"/g, '""'),
      (r.phone || '').replace(/"/g, '""'),
      (r.reason || '').replace(/"/g, '""'),
    ].map(v => `"${v}"`).join(',')).join('\n');
    const blob = new Blob([header + rows], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `rejected_leads_${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const createProviderAccountOptions = (orgExotelAccounts || [])
    .filter(a => (a.direction || 'outbound') !== 'inbound');
  const providerAccountLabel = (provider) => {
    switch ((provider || 'exotel').toLowerCase()) {
      case 'tata':
      case 'smartflo':
      case 'tata_tele':
        return 'Tata Tele';
      case 'twilio':
        return 'Twilio';
      default:
        return 'Exotel';
    }
  };

  return (
    <>
      {/* Create Campaign Modal */}
      {showCreateModal && (
        <div className="modal-overlay" onClick={handleClose} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel" onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
            style={{maxWidth: '680px', width: '95%', maxHeight: '85vh', overflowY: 'auto'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Create New Campaign</h3>

            {/* Template selector */}
            {!hideAiFeatures && (<div style={{marginBottom: '1.5rem'}}>
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
                      }}
                      role="button"
                      tabIndex={0}
                      onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.currentTarget.click(); } }}>
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
            </div>)}

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
                      borderColor: (showNameEmptyError || showNameInvalidError) ? 'rgba(239,68,68,0.6)' : undefined,
                      boxShadow: (showNameEmptyError || showNameInvalidError) ? '0 0 0 3px rgba(239,68,68,0.15)' : undefined,
                    }}
                  />
                  {showNameEmptyError && (
                    <p style={{margin: '4px 0 0', fontSize: '0.78rem', color: '#f87171'}}>
                      {nameError}
                    </p>
                  )}
                  {showNameInvalidError && (
                    <p style={{margin: '4px 0 0', fontSize: '0.78rem', color: '#f87171'}}>
                      Only letters, numbers, spaces and - _ ' . , ( ) # &amp; ! @ are allowed.
                    </p>
                  )}
                </div>
                <div style={{marginBottom: '1.5rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                    Product <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional)</span>
                    {selectedTemplate && <span style={{color: '#60a5fa', fontSize: '0.75rem'}}> — required to apply prompt template</span>}
                  </label>
                  <select className="form-input" value={createForm.product_id}
                    onChange={e => setCreateForm({...createForm, product_id: e.target.value})}
                    style={{width: '100%'}}>
                    <option value="">-- Select Product --</option>
                    {dedupedProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </select>
                </div>
                <div style={{marginBottom: '1.5rem'}}>
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                    Lead Source <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional — where did these leads come from?)</span>
                  </label>
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
                  <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                    Communication Channel <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional — defaults to voice)</span>
                  </label>
                  <select className="form-input" value={createForm.channel || 'voice'}
                    onChange={e => setCreateForm({...createForm, channel: e.target.value})}
                    style={{width: '100%'}}>
                    <option value="voice">📞 Voice Call{!hideAiFeatures && ' (AI Phone)'}</option>
                    {!hideAiFeatures && <option value="whatsapp">💬 WhatsApp (AI Chat)</option>}
                  </select>
                </div>
                {(createForm.channel !== 'whatsapp') && createProviderAccountOptions.length > 0 && (
                  <div style={{marginBottom: '1.5rem'}}>
                    <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                      Provider Account <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional — select saved browser calling credentials)</span>
                    </label>
                    <select className="form-input" value={createForm.exotel_account_id || ''}
                      onChange={e => setCreateForm({...createForm, exotel_account_id: e.target.value ? parseInt(e.target.value) : ''})}
                      style={{width: '100%'}}>
                      <option value="">-- Use default / set later --</option>
                      {createProviderAccountOptions.map(a => (
                        <option key={a.id} value={a.id}>
                          [{providerAccountLabel(a.provider)}] {a.name || a.account_sid} · {a.caller_id || 'no caller ID'}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
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
        <div className="modal-overlay" onClick={() => setShowAddLeadsModal(false)} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel" onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
            style={{maxWidth: '500px', width: '90%', maxHeight: '70vh', display: 'flex', flexDirection: 'column'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Add Leads to Campaign</h3>
            <input
              autoFocus
              type="text"
              placeholder="Search leads by name, phone or company..."
              value={addLeadsSearch}
              onChange={e => searchAvailableLeads(e.target.value)}
              style={{ width: '100%', boxSizing: 'border-box', padding: '9px 14px', borderRadius: 8, border: '1px solid rgba(255,255,255,0.1)', background: 'rgba(255,255,255,0.05)', color: '#e2e8f0', fontSize: 13, fontFamily: 'inherit', outline: 'none', marginBottom: 12 }}
            />
            {addLeadsLoading ? (
              <p style={{color: '#64748b'}}>Searching...</p>
            ) : addLeadsSearch.trim().length < 2 ? (
              <p style={{color: '#64748b'}}>Type at least 2 characters to search.</p>
            ) : addLeadsResults.length === 0 ? (
              <p style={{color: '#64748b'}}>No matching leads found.</p>
            ) : (
              <div style={{flex: 1, overflowY: 'auto', marginBottom: '1rem'}}>
                {addLeadsResults.map(lead => (
                  <label key={lead.id} style={{display: 'flex', alignItems: 'center', gap: '10px', padding: '8px 4px', cursor: 'pointer', borderBottom: '1px solid rgba(255,255,255,0.05)'}}>
                    <input type="checkbox" checked={selectedLeadIds.includes(lead.id)}
                      onChange={() => { toggleLeadSelection(lead.id); setAddLeadsError(''); }} />
                    <span style={{color: '#e2e8f0', fontWeight: 500}}>{lead.first_name} {lead.last_name}</span>
                    <span style={{color: '#64748b', fontSize: '0.8rem'}}>{lead.phone}{lead.company ? ` · ${lead.company}` : ''}</span>
                  </label>
                ))}
              </div>
            )}
            {addLeadsError && (
              <p style={{margin: '0 0 10px', fontSize: '0.82rem', color: '#f87171'}}>⚠ {addLeadsError}</p>
            )}
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={() => { setShowAddLeadsModal(false); setAddLeadsError(''); }}
                style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                Cancel
              </button>
              <button className="btn-primary" disabled={loading}
                onClick={() => { if (selectedLeadIds.length === 0) { setAddLeadsError('Please select at least one lead.'); return; } setAddLeadsError(''); handleAddLeads(); }}>
                {loading ? 'Adding...' : `Add Selected (${selectedLeadIds.length})`}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* CSV Import Modal */}
      {showCsvImportModal && (
        <div className="modal-overlay" onClick={closeCsvImportModal} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel" onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
            style={{maxWidth: '520px', width: '90%', maxHeight: '85vh', overflowY: 'auto'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Import Leads from CSV</h3>
            <p style={{color: '#94a3b8', fontSize: '0.85rem', marginBottom: '0.5rem'}}>
              Upload a CSV with columns: first_name, last_name, phone, company, source. Leads will be created and added to this campaign.
            </p>
            <button
              onClick={() => {
                const csv = 'first_name,last_name,phone,company,source\n';
                const blob = new Blob([csv], { type: 'text/csv' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'callified_leads_template.csv';
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                URL.revokeObjectURL(url);
              }}
              style={{
                background: 'transparent', border: 'none', color: '#818cf8',
                cursor: 'pointer', fontSize: '0.8rem', padding: 0, marginBottom: '1rem',
                textDecoration: 'underline',
              }}>
              Download Template
            </button>

            {csvImportResult && (
              <div style={{marginBottom: '1rem'}}>
                {csvImportResult.error ? (
                  <div style={{padding: '12px 14px', borderRadius: 8, background: 'rgba(239,68,68,0.1)', border: '1px solid rgba(239,68,68,0.3)', color: '#f87171', fontSize: '0.9rem'}}>
                    {csvImportResult.error}
                  </div>
                ) : (
                  <>
                    {(() => {
                      const imported = csvImportResult.imported || 0;
                      const added = csvImportResult.added_to_campaign || 0;
                      const rejectedArrayCount = Array.isArray(csvImportResult.rejected) ? csvImportResult.rejected.length : 0;
                      const rejectedTotal = csvImportResult.rejected_total || rejectedArrayCount;
                      const success = imported > 0 || added > 0;
                      return (
                        <div style={{padding: '12px 14px', borderRadius: 8, marginBottom: '0.75rem', fontSize: '0.9rem',
                          background: success ? 'rgba(16,185,129,0.1)' : 'rgba(148,163,184,0.1)',
                          border: `1px solid ${success ? 'rgba(16,185,129,0.3)' : 'rgba(148,163,184,0.3)' }`,
                          color: success ? '#34d399' : '#94a3b8'}}>
                          Imported {imported} new lead{imported !== 1 ? 's' : ''}, {added} added to campaign.
                          {rejectedTotal > 0 ? ` ${rejectedTotal} rejected.` : ''}
                        </div>
                      );
                    })()}
                    {(() => {
                      const rejected = Array.isArray(csvImportResult.rejected) ? csvImportResult.rejected : [];
                      const errors = Array.isArray(csvImportResult.errors) ? csvImportResult.errors : [];
                      if (rejected.length === 0 && errors.length === 0) return null;
                      const rejectedTotal = csvImportResult.rejected_total || rejected.length;
                      const initialCap = 5;
                      const visible = showAllRejected ? rejected.slice(0, MAX_VISIBLE_REJECTED) : rejected.slice(0, initialCap);
                      const hasMore = rejected.length > initialCap;
                      const truncated = rejected.length > MAX_VISIBLE_REJECTED;
                      const isBackendCapped = rejectedTotal > rejected.length;
                      return (
                        <div style={{border: '1px solid rgba(239,68,68,0.25)', borderRadius: 8, overflow: 'hidden'}}>
                          <div style={{padding: '10px 12px', background: 'rgba(239,68,68,0.08)', color: '#f87171', fontSize: '0.85rem', fontWeight: 600, display: 'flex', justifyContent: 'space-between', alignItems: 'center'}}>
                            <span>Rejected rows {rejectedTotal > 0 ? `(${rejectedTotal})` : ''}</span>
                            {rejected.length > 0 && (
                              <button
                                onClick={() => downloadRejectedCSV(rejected)}
                                style={{background: 'transparent', border: 'none', color: '#f87171', textDecoration: 'underline', fontSize: '0.75rem', cursor: 'pointer'}}>
                                Download CSV
                              </button>
                            )}
                          </div>
                          <div style={{maxHeight: 240, overflowY: 'auto'}}>
                            <table style={{width: '100%', borderCollapse: 'collapse', fontSize: '0.8rem'}}>
                              <thead>
                                <tr style={{background: 'rgba(255,255,255,0.03)'}}>
                                  <th style={{textAlign: 'left', padding: '8px 10px', color: '#94a3b8', fontWeight: 600}}>Row</th>
                                  <th style={{textAlign: 'left', padding: '8px 10px', color: '#94a3b8', fontWeight: 600}}>Name</th>
                                  <th style={{textAlign: 'left', padding: '8px 10px', color: '#94a3b8', fontWeight: 600}}>Phone</th>
                                  <th style={{textAlign: 'left', padding: '8px 10px', color: '#94a3b8', fontWeight: 600}}>Reason</th>
                                </tr>
                              </thead>
                              <tbody>
                                {visible.map((r, i) => (
                                  <tr key={i} style={{borderTop: '1px solid rgba(255,255,255,0.05)'}}>
                                    <td style={{padding: '7px 10px', color: '#e2e8f0'}}>{r.row}</td>
                                    <td style={{padding: '7px 10px', color: '#e2e8f0'}}>{r.first_name || '-'}</td>
                                    <td style={{padding: '7px 10px', color: '#e2e8f0', fontFamily: 'monospace'}}>{r.phone || '-'}</td>
                                    <td style={{padding: '7px 10px', color: '#fca5a5'}}>{r.reason}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                            {hasMore && (
                              <button
                                onClick={() => setShowAllRejected(v => !v)}
                                style={{width: '100%', background: 'rgba(255,255,255,0.03)', border: 'none', borderTop: '1px solid rgba(255,255,255,0.05)', color: '#94a3b8', padding: '8px', fontSize: '0.78rem', cursor: 'pointer'}}>
                                {showAllRejected ? `Show first ${initialCap} rows` : `Show ${Math.min(rejected.length - initialCap, MAX_VISIBLE_REJECTED - initialCap)} more`}
                              </button>
                            )}
                            {truncated && showAllRejected && (
                              <div style={{padding: '8px', color: '#94a3b8', fontSize: '0.75rem', borderTop: '1px solid rgba(255,255,255,0.05)', textAlign: 'center'}}>
                                Showing first {MAX_VISIBLE_REJECTED} of {rejectedTotal} rejected rows. Use the Download CSV button above for the sample list.
                              </div>
                            )}
                            {isBackendCapped && !truncated && (
                              <div style={{padding: '8px', color: '#94a3b8', fontSize: '0.75rem', borderTop: '1px solid rgba(255,255,255,0.05)', textAlign: 'center'}}>
                                Download CSV contains a sample of {rejected.length} rows from {rejectedTotal} total rejected rows.
                              </div>
                            )}
                            {errors.length > 0 && (
                              <div style={{padding: '10px 12px', color: '#fca5a5', fontSize: '0.8rem', borderTop: '1px solid rgba(255,255,255,0.05)'}}>
                                {errors.map((e, i) => <div key={i}>{e}</div>)}
                              </div>
                            )}
                          </div>
                        </div>
                      );
                    })()}
                  </>
                )}
              </div>
            )}

            <div style={{marginBottom: '1rem'}}>
              <input
                ref={csvInputRef}
                type="file"
                accept=".csv"
                onChange={e => { setCsvFile(e.target.files?.[0] || null); if (setCsvImportResult) setCsvImportResult(null); }}
                style={{position: 'absolute', width: 1, height: 1, opacity: 0, pointerEvents: 'none'}}
              />
              <div style={{display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap'}}>
                <button
                  type="button"
                  onClick={() => csvInputRef.current?.click()}
                  style={{
                    background: 'rgba(255,255,255,0.08)', border: '1px solid rgba(255,255,255,0.14)',
                    color: '#e2e8f0', padding: '8px 14px', borderRadius: '8px', cursor: 'pointer',
                    fontSize: '0.85rem', fontWeight: 600,
                  }}>
                  Choose CSV
                </button>
                {csvFile ? (
                  <span style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: '8px',
                    color: '#334155',
                    background: 'rgba(99,102,241,0.08)',
                    border: '1px solid rgba(99,102,241,0.18)',
                    borderRadius: '8px',
                    padding: '7px 8px 7px 10px',
                    fontSize: '0.85rem',
                    fontWeight: 700,
                    maxWidth: '100%',
                  }}>
                    <span style={{wordBreak: 'break-all'}}>{csvFile.name}</span>
                    <button
                      type="button"
                      aria-label="Clear selected CSV"
                      title="Clear selected CSV"
                      onClick={() => {
                        setCsvFile(null);
                        if (setCsvImportResult) setCsvImportResult(null);
                        if (csvInputRef.current) csvInputRef.current.value = '';
                      }}
                      style={{
                        width: 18,
                        height: 18,
                        flex: '0 0 18px',
                        display: 'inline-flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        background: '#ffffff',
                        border: '1px solid rgba(99,102,241,0.18)',
                        borderRadius: '50%',
                        color: '#334155',
                        cursor: 'pointer',
                        fontSize: '0.9rem',
                        lineHeight: 1,
                        padding: 0,
                      }}>
                      &times;
                    </button>
                  </span>
                ) : (
                  <span style={{
                    color: '#94a3b8',
                    border: '1px solid transparent',
                    fontSize: '0.85rem',
                    fontWeight: 500,
                  }}>
                    No file selected
                  </span>
                )}
              </div>
            </div>
            <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
              <button onClick={closeCsvImportModal}
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
        <div className="modal-overlay" onClick={() => { setEditNameTouched(false); setShowEditCampaignModal(false); if (setEditCampaignError) setEditCampaignError(''); }} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel" onClick={e => e.stopPropagation()} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}
            style={{maxWidth: '450px', width: '90%'}}>
            <h3 style={{marginTop: 0, color: '#e2e8f0'}}>Edit Campaign</h3>
            <form onSubmit={e => {
              setEditNameTouched(true);
              if (editNameError) { e.preventDefault(); return; }
              handleSaveEditCampaign(e);
            }}>
              <div style={{marginBottom: '1rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                  Campaign Name <span style={{color: '#ef4444'}}>*</span>
                </label>
                <input className="form-input" placeholder="e.g. AdsGPT March Campaign"
                  value={editCampaignForm.name}
                  maxLength={CAMPAIGN_NAME_MAX_LEN}
                  onChange={e => { setEditNameTouched(true); setEditCampaignForm({...editCampaignForm, name: e.target.value}); if (setEditCampaignError) setEditCampaignError(''); }}
                  onBlur={() => setEditNameTouched(true)}
                  style={{
                    width: '100%',
                    borderColor: (showEditNameError || editCampaignError) ? 'rgba(239,68,68,0.6)' : undefined,
                    boxShadow: (showEditNameError || editCampaignError) ? '0 0 0 3px rgba(239,68,68,0.15)' : undefined,
                  }} />
                {showEditNameError && (
                  <p style={{margin: '4px 0 0', fontSize: '0.78rem', color: '#f87171'}}>{editNameError}</p>
                )}
                {!showEditNameError && editCampaignError && (
                  <p style={{margin: '4px 0 0', fontSize: 12, color: '#f87171'}}>{editCampaignError}</p>
                )}
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                  Product <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional)</span>
                </label>
                <select className="form-input" value={editCampaignForm.product_id}
                  onChange={e => setEditCampaignForm({...editCampaignForm, product_id: e.target.value})}
                  style={{width: '100%'}}>
                  <option value="">-- Select Product --</option>
                  {dedupedProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
              </div>
              <div style={{marginBottom: '1.5rem'}}>
                <label style={{display: 'block', color: '#94a3b8', fontSize: '0.85rem', marginBottom: '4px'}}>
                  Lead Source <span style={{color: '#64748b', fontSize: '0.75rem'}}>(optional)</span>
                </label>
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
                  <option value="voice">📞 Voice Call{!hideAiFeatures && ' (AI Phone)'}</option>
                  {!hideAiFeatures && <option value="whatsapp">💬 WhatsApp (AI Chat)</option>}
                </select>
              </div>
              <div style={{display: 'flex', gap: '10px', justifyContent: 'flex-end'}}>
                <button type="button" onClick={() => { setEditNameTouched(false); setShowEditCampaignModal(false); if (setEditCampaignError) setEditCampaignError(''); }}
                  style={{background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.1)', color: '#94a3b8', padding: '8px 16px', borderRadius: '8px', cursor: 'pointer'}}>
                  Cancel
                </button>
                <button type="submit" className="btn-primary" disabled={loading}>
                  {loading ? 'Saving...' : 'Save Changes'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Edit Lead Modal */}
      {editLead && (
        <div className="modal-overlay" onClick={() => setEditLead(null)} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()} style={{maxWidth: '420px'}} role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); e.currentTarget.click(); } }}>
            <h2 style={{marginTop: 0, marginBottom: '1.5rem'}}>Edit Lead</h2>
            <div className="form-group">
              <label>First Name</label>
              <input className="form-input" value={editForm.first_name}
                onChange={e => { setEditForm({...editForm, first_name: e.target.value}); setEditErrors(prev => ({...prev, first_name: ''})); }}
                style={editErrors.first_name ? {borderColor: '#ef4444'} : {}} />
              {editErrors.first_name && <span style={{color: '#ef4444', fontSize: '0.75rem', marginTop: 4}}>{editErrors.first_name}</span>}
            </div>
            <div className="form-group">
              <label>Last Name</label>
              <input className="form-input" value={editForm.last_name}
                onChange={e => { setEditForm({...editForm, last_name: e.target.value}); setEditErrors(prev => ({...prev, last_name: ''})); }} />
            </div>
            <div className="form-group">
              <label>Phone</label>
              <input className="form-input" value={editForm.phone}
                inputMode="tel"
                onChange={e => { setEditForm({...editForm, phone: e.target.value}); setEditErrors(prev => ({...prev, phone: ''})); }}
                style={editErrors.phone ? {borderColor: '#ef4444'} : {}} />
              {editErrors.phone && <span style={{color: '#ef4444', fontSize: '0.75rem', marginTop: 4}}>{editErrors.phone}</span>}
            </div>
            <div className="form-group">
              <label>Company <span style={{color: '#64748b', fontSize: '0.8rem'}}>(Optional)</span></label>
              <input className="form-input" value={editForm.company || ''} onChange={e => setEditForm({...editForm, company: e.target.value})} placeholder="e.g. Acme Inc." />
            </div>
            <div className="form-group">
              <label>Source</label>
              <input className="form-input" value={editForm.source} onChange={e => setEditForm({...editForm, source: e.target.value})} />
            </div>
            {executives && executives.length > 0 && (
              <div className="form-group">
                <label>Executive</label>
                <select className="form-input" value={editForm.executive_id || ''}
                  onChange={e => setEditForm({...editForm, executive_id: e.target.value ? parseInt(e.target.value, 10) : 0})}>
                  <option value="">— Unassigned —</option>
                  {executives.map(e => <option key={e.id} value={e.id}>{e.name}</option>)}
                </select>
              </div>
            )}
            <div style={{display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '1.5rem'}}>
              <button onClick={() => setEditLead(null)} style={{background: 'transparent', border: '1px solid rgba(255,255,255,0.1)', color: '#cbd5e1', padding: '8px 18px', borderRadius: '8px', cursor: 'pointer'}}>Cancel</button>
              <button className="btn-primary" onClick={() => {
                if (!isValidPhone(editForm.phone || '')) {
                  setEditErrors(prev => ({...prev, phone: PHONE_VALIDATION_MESSAGE}));
                  toast(PHONE_VALIDATION_MESSAGE, 'error');
                  return;
                }
                handleSaveEdit();
              }}>Save</button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
