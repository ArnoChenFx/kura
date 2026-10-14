#!/usr/bin/env bash
# 守望脚本 v2：定期采样 herdr agent 的窗格全文，按内容哈希判断是否真的没动。
# v1 的教训：只取 tail -30 时，若 agent 的活动区域在窗口之外，会误报卡死
# （实测：tail 区 210 秒不变，而 ctx 从 331k 涨到 396k，agent 正常在跑）。
# v2 改法：取 tail -120，并把状态栏的 ctx token 数单独记下来——ctx 变大
# 也算「有进展」，能洗掉哈希窗口的假阳性。
# 用法： bash tools/watch-agent.sh <agent-name> <samples> <interval-sec>
# 日志写在仓库根的 tools/watch-<agent>.log（路径从脚本自身位置推出来，
# 不写死某个人的目录）。
set -u
AGENT="${1:?agent name}"
N="${2:-60}"
IV="${3:-30}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG="$ROOT/tools/watch-${AGENT}.log"
: > "$LOG"
prev=""
prev_ctx=""
stuck=0
for i in $(seq 1 "$N"); do
  ts=$(date +%H:%M:%S)
  snap=$(herdr agent read "$AGENT" --source recent-unwrapped 2>/dev/null)
  # 真正的会话内容（排除状态栏那一行，它刷新慢）。
  body=$(printf '%s' "$snap" | grep -vE 'TPS:|tokens/s|◫ .*%\)' | tail -120)
  h=$(printf '%s' "$body" | sha256sum | cut -c1-12)
  # 状态栏里的上下文 token 数（如 "◫ 396k/1.0M (39.6%)"），能变大就是有进展。
  ctx=$(printf '%s' "$snap" | grep -oE '[0-9]+k?/1\.0M' | tail -1)
  last=$(printf '%s' "$body" | grep -vE '^\s*$' | tail -1 | cut -c1-70)
  if [ "$h" = "$prev" ]; then
    if [ "$ctx" != "$prev_ctx" ]; then
      # 哈希没变但 ctx 在涨：agent 在长思考/长工具调用，算活着。
      stuck=0
      printf '%s hash=%s ctx=%s CHANGED (ctx grew, alive) | %s\n' "$ts" "$h" "$ctx" "$last" >> "$LOG"
    else
      stuck=$((stuck+1))
      printf '%s hash=%s ctx=%s stuck=%d | %s\n' "$ts" "$h" "$ctx" "$stuck" "$last" >> "$LOG"
    fi
  else
    stuck=0
    printf '%s hash=%s ctx=%s ok | %s\n' "$ts" "$h" "$ctx" "$last" >> "$LOG"
  fi
  prev="$h"
  prev_ctx="$ctx"
  if [ "$stuck" -ge 4 ]; then
    printf '%s POSSIBLE-STALL (content and ctx unchanged for %ds)\n' "$ts" "$((stuck*IV))" >> "$LOG"
  fi
  sleep "$IV"
done
printf '%s watch done\n' "$(date +%H:%M:%S)" >> "$LOG"
