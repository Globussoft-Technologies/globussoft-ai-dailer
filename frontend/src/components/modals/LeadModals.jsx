import React from 'react';

export default function LeadModals({
  isModalOpen, setIsModalOpen, handleCreateLead, formData, setFormData, loading,
  editModalOpen, setEditModalOpen, editingLead, handleSaveEdit, editFormData, setEditFormData
}) {
  return (
    <>
      {isModalOpen && (
        <div className="modal-overlay" onClick={() => setIsModalOpen(false)}>
          <div className="glass-panel modal-content" onClick={e => e.stopPropagation()}>
            <h2 style={{marginTop: 0, marginBottom: '2rem'}}>New Lead</h2>
            <form onSubmit={handleCreateLead}>
              <div className="form-group">
                <label>First Name</label>
                <input name="first_name" className="form-input" required value={formData.first_name} onChange={e => setFormData({...formData, first_name: e.target.value})} placeholder="e.g. John" />
              </div>
              <div className="form-group">
                <label>Last Name <span style={{color: '#64748b', fontSize: '0.8rem'}}>(Optional)</span></label>
                <input name="last_name" className="form-input" value={formData.last_name} onChange={e => setFormData({...formData, last_name: e.target.value})} placeholder="e.g. Doe" />
              </div>
              <div className="form-group">
                <label>Phone Number</label>
                <input name="phone" className="form-input" required type="tel" value={formData.phone} onChange={e => setFormData({...formData, phone: e.target.value})} placeholder="+917406317771" />
              </div>
              <div className="form-group">
                <label>Source</label>
                <input name="source" className="form-input" value={formData.source || ""} onChange={e => setFormData({...formData, source: e.target.value})} placeholder="Dashboard" />
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
                <input name="first_name" className="form-input" required value={editFormData.first_name} onChange={e => setEditFormData({...editFormData, first_name: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Last Name <span style={{color: '#64748b', fontSize: '0.8rem'}}>(Optional)</span></label>
                <input name="last_name" className="form-input" value={editFormData.last_name} onChange={e => setEditFormData({...editFormData, last_name: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Phone Number</label>
                <input name="phone" className="form-input" required type="tel" value={editFormData.phone} onChange={e => setEditFormData({...editFormData, phone: e.target.value})} />
              </div>
              <div className="form-group">
                <label>Source</label>
                <input name="source" className="form-input" value={editFormData.source} onChange={e => setEditFormData({...editFormData, source: e.target.value})} />
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
    </>
  );
}
