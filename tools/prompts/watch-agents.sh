#!/usr/bin/env bash
# Watch the two parallel kura agents until both stop working.
# pi's TUI shows "Working" while a turn runs; herdr's agent_status stays "idle"
# for pi, so the pane text is the reliable signal here.
set -u
cd /e/Project

work() {  # $1 = agent name
  herdr agent read "$1" --source recent-unwrapped --lines 30 2>/dev/null | tr -d '\r' \
    | grep -qE "(⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧|⠇|⠏|Working)"
}

newfiles() {  # $1 = worktree
  find "$1" -name '*.go' -newermt '-90 minutes' -not -path '*/.git/*' 2>/dev/null \
    | grep -v 'internal/store/' | wc -l
}

ui_quiet=0; cli_quiet=0
for i in $(seq 1 240); do
  slept=15
  if work kura-ui; then ui_quiet=0; else ui_quiet=$((ui_quiet+1)); fi
  if work kura-cli; then cli_quiet=0; else cli_quiet=$((cli_quiet+1)); fi
  n2=$(newfiles kura-m2); n3=$(newfiles kura-m3)
  echo "[$(date +%H:%M:%S)] ui_quiet=$ui_quiet($n2 files) cli_quiet=$cli_quiet($n3 files)"
  if [ "$ui_quiet" -ge 4 ] && [ "$cli_quiet" -ge 4 ]; then
    echo "BOTH-SETTLED-AT-$(date +%H:%M:%S)"
    herdr agent read kura-ui --source recent-unwrapped --lines 40 2>/dev/null | tr -d '\r' | tail -20
    echo "----------"
    herdr agent read kura-cli --source recent-unwrapped --lines 40 2>/dev/null | tr -d '\r' | tail -20
    exit 0
  fi
  sleep "$slept"
done
echo "WATCHER-TIMEOUT-AT-$(date +%H:%M:%S)"
exit 1
