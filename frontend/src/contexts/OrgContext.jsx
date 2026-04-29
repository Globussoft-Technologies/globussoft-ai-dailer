import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { API_URL } from '../constants/api';
import { useAuth } from './AuthContext';

const OrgContext = createContext(null);

export function OrgProvider({ children }) {
  const { apiFetch, currentUser } = useAuth();

  const [orgs, setOrgs] = useState([]);
  const [selectedOrg, setSelectedOrg] = useState(null);
  const [orgTimezone, setOrgTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [orgProducts, setOrgProducts] = useState([]);

  const fetchOrgProducts = useCallback(async (orgId) => {
    try {
      const res = await apiFetch(`${API_URL}/organizations/${orgId}/products`);
      const list = await res.json();
      // Keep the full list (dups and all) so that id-based lookups —
      // getProductName(campaign.product_id), the campaign header badge —
      // resolve correctly even when a campaign was bound to a duplicate
      // row's id. Dropdowns dedupe at their render site instead.
      setOrgProducts(Array.isArray(list) ? list : []);
    } catch (e) {}
  }, [apiFetch]);

  const fetchOrgs = useCallback(async () => {
    try {
      const res = await apiFetch(`${API_URL}/organizations`);
      const data = await res.json();
      setOrgs(data);
      // Auto-select user's org if only one
      if (data.length === 1 && !selectedOrg) {
        setSelectedOrg(data[0]);
        const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone;
        if (data[0].timezone) {
          setOrgTimezone(data[0].timezone);
        } else {
          setOrgTimezone(browserTz);
          apiFetch(`${API_URL}/organizations/${data[0].id}/timezone`, {
            method: 'PUT', headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ timezone: browserTz })
          }).catch(() => {});
        }
        fetchOrgProducts(data[0].id);
      }
    } catch (e) {}
  }, [apiFetch, selectedOrg, fetchOrgProducts]);

  // Auto-fetch orgs when user is authenticated
  useEffect(() => {
    if (currentUser) {
      fetchOrgs();
    }
  }, [currentUser]);

  return (
    <OrgContext.Provider value={{
      orgs, setOrgs,
      selectedOrg, setSelectedOrg,
      orgTimezone, setOrgTimezone,
      orgProducts, setOrgProducts,
      fetchOrgs, fetchOrgProducts
    }}>
      {children}
    </OrgContext.Provider>
  );
}

export function useOrg() {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error('useOrg must be used within OrgProvider');
  return ctx;
}
