#!/usr/bin/env bash
# ingest-scans.sh — Parse scan JSON files and create snapshots in the DB.
#
# Usage:
#   ./scripts/ingest-scans.sh                  # ingest all scan files
#   ./scripts/ingest-scans.sh cortex           # ingest one repo
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
SCAN_DIR="$PROJECT_DIR/data/scans"
SE="$PROJECT_DIR/mentat"

if [[ ! -d "$SCAN_DIR" ]]; then
  echo "No scan directory found at $SCAN_DIR"
  exit 1
fi

FILTER="${1:-}"
COUNT=0

for scan_file in "$SCAN_DIR"/*.json; do
  [[ -f "$scan_file" ]] || continue

  repo_id=$(python3 -c "import json; print(json.load(open('$scan_file'))['repo_id'])" 2>/dev/null || continue)

  if [[ -n "$FILTER" && "$repo_id" != "$FILTER" ]]; then
    continue
  fi

  # Extract metrics from scc output
  metrics=$(python3 -c "
import json, sys
d = json.load(open('$scan_file'))
scc = d.get('scc', [])
git = d.get('git', {})

total_loc = sum(lang.get('Code', 0) for lang in scc if isinstance(lang, dict))
total_files = sum(lang.get('Count', 0) for lang in scc if isinstance(lang, dict))
test_files = sum(1 for lang in scc if isinstance(lang, dict) for _ in [] if 'test' in lang.get('Name','').lower())

contribs = git.get('contributors', 0)
commits_30d = git.get('commits_30d', 0)

# Build flags
flags = []
if total_loc: flags.append(f'--loc {total_loc}')
if total_files: flags.append(f'--files {total_files}')
if contribs: flags.append(f'--contributors {contribs}')
if commits_30d: flags.append(f'--commits-30d {commits_30d}')
print(' '.join(flags))
" 2>/dev/null || echo "")

  if [[ -z "$metrics" ]]; then
    echo "SKIP $repo_id — could not parse scan"
    continue
  fi

  echo "Ingesting $repo_id: $metrics"
  eval "$SE snapshot take $repo_id $metrics" 2>/dev/null || echo "  WARNING: snapshot failed for $repo_id"
  COUNT=$((COUNT + 1))
done

echo "Ingested $COUNT scan(s)"
