#!/usr/bin/env bash
# Stop the local stack. Pass --volumes to also drop the database.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$ROOT"
$DC down --remove-orphans "$@"
