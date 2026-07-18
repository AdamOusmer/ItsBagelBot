#!/usr/bin/env bash
# Compare retained R1/R3 and local/preferred benchmark summaries. This script
# is read-only: it never connects to Kubernetes or NATS.
set -euo pipefail

usage() {
	cat <<'USAGE'
usage: r3-matrix-report.sh [--report-only] SUMMARY_OR_RESULTS_DIR [...]

Reads summary JSON emitted by r3-isolated-tune.sh or r3-120k.sh and prints one
normalized matrix. By default it exits non-zero until a node-local R3 run has
sustained 90,000 offered events/s for 30 minutes while acknowledging at least
89,100/s, keeping loaded PubAck p99 <= 2ms and broker CPU <= 75%.

Use --report-only to compare incomplete/calibration runs without qualifying the
90k operating gate.
USAGE
}

report_only=false
inputs=()
for argument in "$@"; do
	case $argument in
		--report-only) report_only=true ;;
		-h|--help) usage; exit 0 ;;
		-*) echo "unknown option: $argument" >&2; usage >&2; exit 2 ;;
		*) inputs+=("$argument") ;;
	esac
done

if ((${#inputs[@]} == 0)); then
	usage >&2
	exit 2
fi
command -v jq >/dev/null 2>&1 || { echo "missing required command: jq" >&2; exit 1; }

summaries=()
for input in "${inputs[@]}"; do
	if [[ -d $input ]]; then
		while IFS= read -r summary; do summaries+=("$summary"); done < <(
			find "$input" -type f \( -name 'summary.json' -o -name 'summary-operating-*.json' \) -print | sort
		)
	elif [[ -f $input ]]; then
		summaries+=("$input")
	else
		echo "summary path does not exist: $input" >&2
		exit 2
	fi
done

if ((${#summaries[@]} == 0)); then
	echo "no benchmark summary JSON found" >&2
	exit 2
fi

report=$(jq -s '
	def normalized:
		select((.aggregate_messages_per_second // null) != null) |
		{
			trial:(.trial // "isolated"),
			stream_replicas:(.stream_replicas // .replicas // 3),
			publish_target:(.publish_target // "unknown"),
			publish_mode:(.publish_mode // .mode // "unknown"),
			route_compression:(.route_compression // "production"),
			target_messages_per_second,
			acknowledged_messages_per_second:.aggregate_messages_per_second,
			duration_seconds:(.conservative_duration_ms / 1000),
			puback_p50_ms:.worst_node_puback_p50_ms,
			puback_p95_ms:.worst_node_puback_p95_ms,
			puback_p99_ms:.worst_node_puback_p99_ms,
			puback_max_ms,
			broker_peak_cpu_pct:(.broker_metrics.peak_cpu_limit_utilization_pct // null),
			broker_peak_memory_bytes:(.broker_metrics.peak_memory_bytes // null),
			broker_peak_route_pending_bytes:(.broker_metrics.peak_route_pending_bytes // null),
			broker_peak_follower_lag:(.broker_metrics.peak_follower_lag // null),
			errors:(.errors // 0),
			timeouts:(.timeouts // 0),
			reconnects:(.reconnects // 0),
			passed:(.passed // false)
		};
	[.[] | normalized] as $runs |
	{
		qualification_gates:{
			stream_replicas:3,
			publish_target:"local",
			offered_messages_per_second:90000,
			minimum_acknowledged_messages_per_second:89100,
			minimum_duration_seconds:1800,
			maximum_loaded_puback_p99_ms:2,
			maximum_broker_cpu_pct:75
		},
		runs:($runs | sort_by(.stream_replicas,.publish_target,.target_messages_per_second,.publish_mode)),
		qualified_runs:[$runs[] | select(
			.stream_replicas == 3 and
			.publish_target == "local" and
			.target_messages_per_second == 90000 and
			.acknowledged_messages_per_second >= 89100 and
			.duration_seconds >= 1800 and
			.puback_p99_ms <= 2 and
			.broker_peak_cpu_pct != null and .broker_peak_cpu_pct <= 75 and
			.errors == 0 and .timeouts == 0 and .reconnects == 0 and .passed == true
		)],
		qualified:false
	} | .qualified = ((.qualified_runs | length) > 0)
' "${summaries[@]}")

jq . <<<"$report"
if [[ $report_only == false ]] && ! jq -e '.qualified == true' >/dev/null <<<"$report"; then
	echo "R3 90k/30m sustain gate is not yet qualified" >&2
	exit 1
fi
