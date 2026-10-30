// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export { SUB } from './services/subjects';
export {
  SCOPES,
  fabric,
  invalidate,
  broadcastInvalidation,
  evictRenamed,
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
  retryLifecycle,
  setActive,
  setCommandsPage,
  setCursor,
  setLeaderboardPage,
  setLocale,
  setOnboarded,
  userCommandsPage,
  userCursor,
  userLeaderboardPage,
  userLocale,
  type AccountState,
  type AccountStatus
} from './services/account-state';
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
  billingState,
  checkoutBasketCreate,
  type BillingGrantSource,
  type BillingState,
  type CheckoutBasket,
  type CheckoutPackageType,
  type CheckoutRequest
} from './services/billing';
export { auditDashboardImpersonation, auditImpersonation, type AuditEntry } from './services/audit';
export {
  notificationMarkPeeked,
  notificationMarkRead,
  notificationsForUser,
  type NotificationWire,
  type NotificationsForUser
} from './services/notifications';
