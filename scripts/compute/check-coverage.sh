#!/bin/bash
# Statement coverage gate. No exclusions are removed from the overall score.
set -euo pipefail
profile="${1:?usage: check-coverage.sh PROFILE [OVERALL_PERCENT]}"
floor="${2:-80}"
[[ -f "$profile" && "$floor" =~ ^[0-9]+([.][0-9]+)?$ ]] || { echo 'Invalid coverage profile/floor' >&2; exit 1; }
awk -v floor="$floor" '
BEGIN {
  critical["/internal/compute/control"] = 90
  critical["/internal/compute/runtime/docker"] = 90
  critical["/internal/compute/guest"] = 85
  critical["/internal/compute/browser"] = 85
  critical["/internal/compute/neko"] = 80
}
NR == 1 { if ($0 !~ /^mode: (atomic|count|set)$/) { print "Invalid coverage header" > "/dev/stderr"; bad = 1 }; next }
{
  if (NF != 3 || $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/) { print "Malformed coverage record" > "/dev/stderr"; bad = 1; next }
  file=$1; sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", file)
  package=file; sub(/\/[^\/]+$/, "", package)
  statements[package]+=$2; total+=$2
  if ($3 > 0) { covered[package]+=$2; hit+=$2 }
}
END {
  if (bad || total == 0) { print "Missing or invalid statement coverage" > "/dev/stderr"; exit 1 }
  percentage=100*hit/total
  printf "Overall statement coverage: %.2f%% (%d/%d), required %.2f%%\n", percentage,hit,total,floor
  if (percentage + 0.000001 < floor) failed=1
  for (suffix in critical) {
    found=0
    for (package in statements) {
      if (length(package)>=length(suffix) && substr(package,length(package)-length(suffix)+1)==suffix) {
        found=1
        score=statements[package] ? 100*covered[package]/statements[package] : 0
        printf "Critical package %s: %.2f%%, required %.2f%%\n", suffix,score,critical[suffix]
        if (score + 0.000001 < critical[suffix]) failed=1
      }
    }
    if (!found) { printf "Required critical package absent: %s\n",suffix > "/dev/stderr"; failed=1 }
  }
  if (failed) { print "Coverage policy FAILED" > "/dev/stderr"; exit 1 }
}' "$profile"
