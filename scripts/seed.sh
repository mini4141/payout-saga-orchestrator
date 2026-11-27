#!/usr/bin/env bash
# Load demo data: 5 merchants with opening balances (idempotent). Also runs automatically on dev-up.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
cd "$ROOT"
$DC run --rm migrate migrate seed
psql_q "select id, name, balance_minor from merchants order by id"
