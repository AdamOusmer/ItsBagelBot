// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import "math"

func sumEarnedPoints(current, incoming int64) int64 {
	if incoming > 0 && current > math.MaxInt64-incoming {
		return math.MaxInt64
	}
	return current + incoming
}

// Automatic credits stop at the signed BIGINT limit. The subtraction guard
// prevents SQL from evaluating an overflowing addition, including a concurrent
// absolute set of the balance while the award waits for its row lock.
func earnedPointsExpression(incoming string) string {
	return "CASE WHEN " + incoming + " > 0 THEN CASE WHEN points > 9223372036854775807 - " + incoming +
		" THEN 9223372036854775807 ELSE points + " + incoming + " END ELSE points + " + incoming + " END"
}

func (r *Loyalty) earnedBalanceConflict() string {
	if r.dialect == "sqlite3" {
		return " ON CONFLICT(user_id,viewer_id) DO UPDATE SET points=" + earnedPointsExpression("excluded.points") +
			",watch_seconds=watch_seconds+excluded.watch_seconds,viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END,viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END,updated_at=excluded.updated_at"
	}
	return " ON DUPLICATE KEY UPDATE points=" + earnedPointsExpression("VALUES(points)") +
		",watch_seconds=watch_seconds+VALUES(watch_seconds),viewer_login=IF(VALUES(viewer_login)='',viewer_login,VALUES(viewer_login)),viewer_name=IF(VALUES(viewer_name)='',viewer_name,VALUES(viewer_name)),updated_at=VALUES(updated_at)"
}
