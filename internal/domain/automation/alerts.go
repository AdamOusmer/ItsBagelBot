// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import "slices"

const AlertsModuleName = "alerts"

var alertEnableKeys = []string{"followEnabled", "subEnabled", "giftEnabled", "cheerEnabled", "raidEnabled"}

func IsAlertEnableKey(key string) bool { return slices.Contains(alertEnableKeys, key) }
