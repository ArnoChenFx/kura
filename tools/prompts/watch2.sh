#!/usr/bin/env bash
set -u
cd /e/Project
work() { herdr agent read "$1" --source recent-unwrapped --lines 25 2>/dev/null | tr -d '\r' | grep -qE "(⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧|⠇|⠏|Working)"; }
for i in $(seq 1 300); do
  a=0; b=0
  work kura-ci || a=1
  work kura-selfupd || b=1
  q=$(git -C kura-m5 status --porcelain -uall 2>/dev/null | wc -l)
  r=$(git -C kura-m6 status --porcelain -uall 2>/dev/null | wc -l)
  echo "[$(date +%H:%M:%S)] ci_idle=$a(m5:$q) upd_idle=$b(m6:$r)"
  if [ "$a" = 1 ] && [ "$b" = 1 ] && [ "$q" != 0 ] && [ "$r" != 0 ]; then echo BOTH-SETTLED; exit 0; fi
  sleep 25
done
echo TIMEOUT
