# Shared New Relic boundary

This package isolates New Relic agent 1.42.0 and provides bounded attribute
policies and trace propagation. Services supply constant transaction, span,
event and metric names, and declare a finite enum or numeric policy for every
exported attribute. Trace propagation includes only vendor trace headers.
Metrics services aggregate counters in ETS and export deltas on a timer; a failed
export restores the pending delta. Gateway local snapshots remain cumulative.

`runtime_config/2` disables harvesting without a license, and disables automatic
instrumentation, query and argument collection, error collection, and logs in
context by default. It rejects conflicting SDK environment overrides and direct
log export. Ingress explicitly opts into its existing `:forwarder` mode, which
adds New Relic context for an external log forwarder; application logs can then
be exported by that forwarder and must be managed under the ingress logging
policy. Ingress also keeps error collection with `error_collector: true`.
Gateway defaults to disabled. Set `NEW_RELIC_APP_NAME` and
`NEW_RELIC_LICENSE_KEY` for reporting. No license means no agent harvest; no
credentials are used in the package tests.
