#!/usr/bin/env bash
# Build the process binaries into .artifacts/bin, never into the checkout root.
#
#   scripts/build.sh            # hcmnext, scheduler, worker, migrate, hcmctl, projector
#   scripts/build.sh hcmnext    # one command
set -eu
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/.artifacts/bin"
mkdir -p "$out"
cmds=("$@")
[ ${#cmds[@]} -eq 0 ] && cmds=(hcmnext scheduler worker migrate hcmctl projector)
cd "$root"
for c in "${cmds[@]}"; do
  go build -o "$out/$c.exe" "./cmd/$c"
  echo "built $out/$c.exe"
done
