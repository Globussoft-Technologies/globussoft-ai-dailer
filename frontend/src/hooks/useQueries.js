import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../contexts/AuthContext';
import { API_URL } from '../constants/api';

async function fetchJson(apiFetch, url) {
  const res = await apiFetch(url);
  if (!res.ok) {
    const text = await res.text().catch(() => `HTTP ${res.status}`);
    throw new Error(text);
  }
  const data = await res.json();
  return data;
}

export function useCampaigns() {
  const { apiFetch } = useAuth();
  return useQuery({
    queryKey: ['campaigns'],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/campaigns`),
  });
}

export function useCampaign(id) {
  const { apiFetch } = useAuth();
  return useQuery({
    queryKey: ['campaign', id],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/campaigns/${id}`),
    enabled: Boolean(id),
  });
}

export function useLeads(campaignId, params = {}) {
  const { apiFetch } = useAuth();
  const searchParams = new URLSearchParams();
  if (params.page !== undefined) searchParams.set('page', String(params.page));
  if (params.limit !== undefined) searchParams.set('limit', String(params.limit));
  if (params.search) searchParams.set('search', params.search);
  if (params.executiveIds?.length) searchParams.set('executive_ids', params.executiveIds.join(','));
  if (params.scheduleFrom) searchParams.set('scheduled_from', params.scheduleFrom);
  if (params.scheduleTo) searchParams.set('scheduled_to', params.scheduleTo);
  const query = searchParams.toString() ? `?${searchParams.toString()}` : '';

  return useQuery({
    queryKey: ['campaign', campaignId, 'leads', params],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/campaigns/${campaignId}/leads${query}`),
    enabled: Boolean(campaignId),
  });
}

export function useCallLogs(campaignId, params = {}) {
  const { apiFetch } = useAuth();
  const searchParams = new URLSearchParams();
  if (params.executiveIds?.length) searchParams.set('executive_ids', params.executiveIds.join(','));
  const query = searchParams.toString() ? `?${searchParams.toString()}` : '';

  return useQuery({
    queryKey: ['campaign', campaignId, 'callLogs', params],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/campaigns/${campaignId}/call-log${query}`),
    enabled: Boolean(campaignId),
  });
}

export function useAgentReport(filters = {}) {
  const { apiFetch } = useAuth();
  const searchParams = new URLSearchParams();
  if (filters.from) searchParams.set('from', filters.from);
  if (filters.to) searchParams.set('to', filters.to);
  if (filters.campaignId) searchParams.set('campaign_id', String(filters.campaignId));
  const query = searchParams.toString() ? `?${searchParams.toString()}` : '';

  return useQuery({
    queryKey: ['agentReport', filters],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/analytics/agent-lead-summary${query}`),
  });
}

export function useOrganizations() {
  const { apiFetch } = useAuth();
  return useQuery({
    queryKey: ['organizations'],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/organizations`),
  });
}

export function useOrgProducts(orgId) {
  const { apiFetch } = useAuth();
  return useQuery({
    queryKey: ['org', orgId, 'products'],
    queryFn: () => fetchJson(apiFetch, `${API_URL}/organizations/${orgId}/products`),
    enabled: Boolean(orgId),
  });
}
