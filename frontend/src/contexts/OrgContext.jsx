import React, { createContext, useContext, useState, useEffect, useCallback } from 'react';
import { useOrganizations, useOrgProducts } from '../hooks/useQueries';
import { queryClient } from '../queryClient';
import { API_URL } from '../constants/api';
import { useAuth } from './AuthContext';

const OrgContext = createContext(null);

export function OrgProvider({ children }) {
  const { apiFetch, currentUser } = useAuth();
  const { data: orgs = [], refetch: refetchOrgs } = useOrganizations();
  const [selectedOrg, setSelectedOrg] = useState(null);
  const [orgTimezone, setOrgTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone);
  const [orgProductsOverride, setOrgProductsOverride] = useState(null);

  const orgId = selectedOrg?.id;
  const { data: orgProducts = [] } = useOrgProducts(orgId);

  // Reset org state when the logged-in user changes or logs out.
  useEffect(() => {
    if (!currentUser) {
      setSelectedOrg(null);
      setOrgProductsOverride(null);
      setOrgTimezone(Intl.DateTimeFormat().resolvedOptions().timeZone);
    }
  }, [currentUser]);

  // Auto-select single org, keep existing selection valid, and sync timezone.
  useEffect(() => {
    if (!Array.isArray(orgs) || orgs.length === 0) return;

    if (selectedOrg && !orgs.find(o => o.id === selectedOrg.id)) {
      setSelectedOrg(null);
      setOrgProductsOverride(null);
    }

    if (orgs.length === 1 && !selectedOrg) {
      const org = orgs[0];
      setSelectedOrg(org);
      const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone;
      if (org.timezone) {
        setOrgTimezone(org.timezone);
      } else {
        setOrgTimezone(browserTz);
        apiFetch(`${API_URL}/organizations/${org.id}/timezone`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ timezone: browserTz })
        }).catch(() => {});
      }
    }
  }, [orgs, selectedOrg, apiFetch]);

  const fetchOrgs = useCallback(() => {
    refetchOrgs();
  }, [refetchOrgs]);

  const fetchOrgProducts = useCallback((id) => {
    queryClient.invalidateQueries({ queryKey: ['org', id, 'products'] });
  }, []);

  const setOrgProducts = useCallback((value) => {
    setOrgProductsOverride(value);
  }, []);

  return (
    <OrgContext.Provider value={{
      orgs, setOrgs: () => {},
      selectedOrg, setSelectedOrg,
      orgTimezone, setOrgTimezone,
      orgProducts: orgProductsOverride ?? orgProducts,
      setOrgProducts,
      fetchOrgs, fetchOrgProducts
    }}>
      {children}
    </OrgContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useOrg() {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error('useOrg must be used within OrgProvider');
  return ctx;
}
