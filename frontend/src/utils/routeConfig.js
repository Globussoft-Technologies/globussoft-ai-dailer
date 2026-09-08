import React from 'react';

// Eagerly loaded pages (keep in main bundle).
import CrmPage from '../pages/CrmPage';
import CampaignsPage from '../pages/CampaignsPage';
import ManualDialPage from '../pages/ManualDialPage';
import SettingsPage from '../pages/SettingsPage';
import OpsPage from '../pages/OpsPage';
import CheckInPage from '../pages/CheckInPage';
import ScheduledCallsPage from '../pages/ScheduledCallsPage';
import InteractionHistoryPage from '../pages/InteractionHistoryPage';
import AgentPresencePage from '../pages/AgentPresencePage';
import ProductsPage from '../pages/ProductsPage';

const DEFAULT_ROLES = ['Admin', 'SuperAdmin', 'TeamLeader', 'Agent', 'Executive'];
const ADMIN_ROLES = ['Admin', 'SuperAdmin'];

// Lazy-loaded heavy pages.
const AnalyticsPage = React.lazy(() => import('../pages/AnalyticsPage'));
const AgentReportPage = React.lazy(() => import('../pages/AgentReportPage'));
const TeamPage = React.lazy(() => import('../pages/TeamPage'));
const UserManagementPage = React.lazy(() => import('../pages/UserManagementPage'));
const ReceptionistPage = React.lazy(() => import('../pages/ReceptionistPage'));
const LogsPage = React.lazy(() => import('../pages/LogsPage'));
const MonitorPage = React.lazy(() => import('../pages/MonitorPage'));
const KnowledgePage = React.lazy(() => import('../pages/KnowledgePage'));
const SandboxPage = React.lazy(() => import('../pages/SandboxPage'));
const CampaignProgressPage = React.lazy(() => import('../pages/CampaignProgressPage'));
const ExotelAccountsPage = React.lazy(() => import('../pages/ExotelAccountsPage'));
const ExecutivesPage = React.lazy(() => import('../pages/ExecutivesPage'));
const IntegrationsPage = React.lazy(() => import('../pages/IntegrationsPage'));
const WhatsAppPage = React.lazy(() => import('../pages/WhatsAppPage'));
const BillingPage = React.lazy(() => import('../pages/BillingPage'));
const SubscriptionsPage = React.lazy(() => import('../pages/SubscriptionsPage'));
const FeatureFlagsPage = React.lazy(() => import('../pages/FeatureFlagsPage'));
const DndPage = React.lazy(() => import('../pages/DndPage'));

export const routes = [
  { path: '/crm', element: CrmPage, roles: DEFAULT_ROLES },
  { path: '/campaigns', element: CampaignsPage, roles: DEFAULT_ROLES },
  { path: '/campaigns/:campaignId', element: CampaignsPage, roles: DEFAULT_ROLES },
  { path: '/manual-dial', element: ManualDialPage, roles: DEFAULT_ROLES },
  { path: '/ops', element: OpsPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/analytics', element: AnalyticsPage, roles: ADMIN_ROLES },
  { path: '/whatsapp', element: WhatsAppPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/integrations', element: IntegrationsPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/monitor', element: MonitorPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/knowledge', element: KnowledgePage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/sandbox', element: SandboxPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/products', element: ProductsPage, roles: ADMIN_ROLES },
  { path: '/settings', element: SettingsPage, roles: DEFAULT_ROLES },
  { path: '/logs', element: LogsPage, aiFeatures: true },
  { path: '/checkin', element: CheckInPage, roles: DEFAULT_ROLES },
  { path: '/billing', element: BillingPage, aiFeatures: true },
  { path: '/dnd', element: DndPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/scheduled', element: ScheduledCallsPage, roles: DEFAULT_ROLES, aiFeatures: true },
  { path: '/interaction-history', element: InteractionHistoryPage, roles: DEFAULT_ROLES },
  { path: '/agent-presence', element: AgentPresencePage, roles: ADMIN_ROLES },
  { path: '/agent-report', element: AgentReportPage, roles: ADMIN_ROLES },
  { path: '/campaign-progress', element: CampaignProgressPage, roles: ADMIN_ROLES, aiFeatures: true },
  { path: '/team', element: TeamPage, roles: ADMIN_ROLES },
  { path: '/user-management', element: UserManagementPage, roles: ADMIN_ROLES },
  { path: '/ai-receptionist', element: ReceptionistPage, aiFeatures: true },
  { path: '/exotel-accounts', element: ExotelAccountsPage, roles: DEFAULT_ROLES },
  { path: '/executives', element: ExecutivesPage, roles: DEFAULT_ROLES },
  { path: '/subscriptions', element: SubscriptionsPage, roles: ADMIN_ROLES },
  { path: '/feature-flags', element: FeatureFlagsPage, roles: ADMIN_ROLES },
];
