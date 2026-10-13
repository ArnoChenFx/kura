#!/usr/bin/env bash
set -u
cd /e/Project
work() { herdr agent read "$1" --source recent-unwrapped --lines 25 2>/dev/null | tr -d '\r' | grep -qE "(⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧|⠇|⠏|Working)"; }
post() { read -r -d '' _ <<'P'
P
}
for i in $(seq 1 360); do
  ci=0; up=0; ui=0
  work kura-ci || ci=1
  work kura-selfupd || up=1
  work kura-ui2 || ui=1
  a=$(git -C kura-m5 status --porcelain -uall 2>/dev/null | wc -l)
  b=$(git -C kura-m6 status --porcelain -uall 2>/dev/null | wc -l)
  c=$(git -C kura-m7 status --porcelain -uall 2>/dev/null | wc -l)
  echo "[$(date +%H:%M:%S)] ci_idle=$ci(m5:$a) upd_idle=$up(m6:$b) ui_idle=$ui(m7:$c)"
  sleep 30
done
echo TIMEOUT
