// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import pkg_valkey "ItsBagelBot/pkg/valkey"

var (
	loyaltyDiscoverySaveScript = pkg_valkey.SaveQueuePage
	loyaltyDiscoveryPopScript  = pkg_valkey.PopQueueFront
	loyaltyArmScript           = pkg_valkey.ArmWindow
	loyaltyDisarmScript        = pkg_valkey.DisarmWindow
	loyaltyDueScript           = pkg_valkey.DueWindows
	loyaltyClaimScript         = pkg_valkey.ClaimWindow
	loyaltySavePageScript      = pkg_valkey.SaveWindowPage
	loyaltyCommitPageScript    = pkg_valkey.CommitWindowPage
	loyaltyConfirmScript       = pkg_valkey.ConfirmWindow
	loyaltyStopScript          = pkg_valkey.StopWindow
	loyaltyRetryScript         = pkg_valkey.RetryWindow
	loyaltyAbandonScript       = pkg_valkey.AbandonWindow
)
