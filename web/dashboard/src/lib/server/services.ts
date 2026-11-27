// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export { SUB } from './services/subjects';
export {
  SCOPES,
  fabric,
  invalidate,
  broadcastInvalidation,
  evictRenamed,
  onLoyaltyFreeze,
  onRename,
  startInvalidationListener,
  userPrefixes,
  type BoardChange
} from './services/fabric';
export {
  delegationAccess,
  delegationConsume,
  delegationCreate,
  delegationGet,
  delegationList,
  delegationOptOut,
  delegationRevoke,
  delegationUpdate,
  type DelegationGrant
} from './services/delegation';
export {
  channelSubState,
  publishEventSub,
  publishEventSubEnsureOptional,
  publishEventSubReconnect,
  type ChannelSubState,
  type SubStateOptions,
  type SubVerification
} from './services/eventsub';
export {
  accountState,
  freshAccountState,
  pageToggles,
  retryLifecycle,
  setActive,
  setCommandsPage,
  setCursor,
  setLeaderboardPage,
  setLocale,
  setOnboarded,
  setSongQueuePage,
  userCommandsPage,
  userCursor,
  userLeaderboardPage,
  userLocale,
  userSongQueuePage,
  type AccountState,
  type AccountStatus
} from './services/account-state';
export { setMarketing, setTermsConsent } from './services/consent';
export {
  marketingLink,
  recordDelivery,
  setMarketingLink,
  suppressAddress,
  type Delivery,
  type DeliveryEvent,
  type LinkChange,
  type MarketingLink,
  type Suppression,
  type SuppressionReason
} from './services/marketing-link';
export {
  deleteSelf,
  hasGrant,
  isBanned,
  resolveLogin,
  saveGrant,
  syncIdentity,
  tier,
  type ResolvedChannel
} from './services/account';
export {
  billingLinks,
  billingState,
  checkoutBasketCreate,
  type BillingGrantSource,
  type BillingLinks,
  type BillingState,
  type CheckoutBasket,
  type CheckoutPackageType,
  type CheckoutRequest
} from './services/billing';
export { auditDashboardImpersonation, auditImpersonation, type AuditEntry } from './services/audit';
export {
  deviceCodeCreate,
  deviceCodeExchange,
  deviceTokenRefresh,
  deviceGrantList,
  deviceGrantRevoke,
  deviceGrantRevokeAll,
  type DeviceTokens,
  type DeviceGrantView,
  type TokenOutcome,
  type TokenOauthError
} from './services/device-auth';
export {
  notificationMarkPeeked,
  notificationMarkRead,
  notificationsForUser,
  type NotificationWire,
  type NotificationsForUser
} from './services/notifications';
