#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
topology="$root_dir/topology.json"

command -v doppler >/dev/null
command -v jq >/dev/null

failed=0

while IFS=$'\t' read -r project config expected_names; do
  metadata="$(doppler configs get --project "$project" --config "$config" --json --no-check-version)"
  if [[ "$(jq -r '.inheritable' <<<"$metadata")" != "true" ]]; then
    echo "$project/$config: not inheritable"
    failed=1
  fi

  actual_names="$({ doppler secrets --only-names --project "$project" --config "$config" --json --no-check-version; } \
    | jq -cS '[keys[] | select(startswith("DOPPLER_") | not)]')"
  if [[ "$actual_names" != "$expected_names" ]]; then
    echo "$project/$config: name set differs"
    failed=1
  else
    echo "$project/$config: PASS"
  fi
done < <(jq -r '
  .parents | to_entries[] as $project |
  $project.value.configs | to_entries[] |
  [$project.key, .key, (.value | sort | tojson)] | @tsv
' "$topology")

while IFS=$'\t' read -r project config expected_names; do
  actual_names="$({ doppler secrets --only-names --project "$project" --config "$config" --json --no-check-version; } \
    | jq -cS '[keys[] | select(startswith("DOPPLER_") | not)]')"
  if [[ "$actual_names" != "$expected_names" ]]; then
    echo "$project/$config: integration name set differs"
    failed=1
  else
    echo "$project/$config: PASS"
  fi
done < <(jq -r '
  .integrations | to_entries[] |
  [.key, .value.config, (.value.names | sort | tojson)] | @tsv
' "$topology")

while IFS=$'\t' read -r project config expected_refs; do
  actual_refs="$(doppler configs get --project "$project" --config "$config" --json --no-check-version \
    | jq -c '[.inherits[]? | .project + "." + .config] | sort')"
  if [[ "$actual_refs" != "$expected_refs" ]]; then
    echo "$project/$config: inheritance differs"
    failed=1
  else
    echo "$project/$config: PASS"
  fi
done < <(jq -r '
  . as $top |
  .services | to_entries[] as $service |
  $top.profiles[$service.value.inheritProfile] | to_entries[] |
  [$service.key, .key, (.value | sort | tojson)] | @tsv
' "$topology")

exit "$failed"
