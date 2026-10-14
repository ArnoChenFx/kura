#!/usr/bin/env bash
# 守望 i18n agent
exec bash "$(dirname "$0")/watch-agent.sh" i18n "${1:-120}" "${2:-30}"
