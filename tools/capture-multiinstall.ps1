# 启窗验证「一个仓库多选安装 + 默认名取 SKILL.md 的 name」。
#
# 全程隔离：
#   * USERPROFILE / HOME 指向本脚本的临时目录，不碰真实的 ~/.kura、~/.agents；
#   * github 地址经隔离 git 配置的 insteadOf 重写指向本地 bare 仓库，不联网；
#   * 用 UI Automation 驱动真实窗口（点按钮、填仓库、勾候选），PrintWindow 截图。
#
# 用法（在 worktree 根目录运行）：
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-multiinstall.ps1 `
#     -Exe build/windows-amd64/kura.exe -OutDir tools/multiinstall-window
#
# 产物：outdir/candidates-1-checked.png（候选列表 + 勾选一个 + 主按钮计数）、
#       outdir/installed.png（装完后的主窗口）、outdir/evidence.txt（文字证据）。
param(
  [Parameter(Mandatory = $true)][string]$Exe,
  [string]$OutDir = "tools/multiinstall-window",
  [string]$RepoName = "kura-demo/multi",
  [string]$RepoName2 = "kura-demo/other",
  [int]$WaitMs = 6000,
  [switch]$Dump
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Win {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h, int x, int y, int w, int ht, bool repaint);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
  [DllImport("shcore.dll")] public static extern int SetProcessDpiAwareness(int v);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
}
"@
try { [Win]::SetProcessDpiAwareness(2) | Out-Null } catch {}

$root = (Get-Location).Path
$Exe = (Resolve-Path $Exe).Path
$out = Join-Path $root $OutDir
New-Item -ItemType Directory -Force -Path $out | Out-Null
# 只清本脚本自己造的产物（fixtures 与它要写的 PNG），别动同目录下别人的东西：
# 无头渲染的 install-candidates.png 也放在这里（见 TestRenderInstallCandidatesShot），
# 整个目录删掉会把那条证据链一起删了。
foreach ($p in @("home", "src-repo", "src-repo2", "gitconfig", "evidence.txt", "dump.png",
    "candidates-1-checked.png", "candidates-2-checked.png", "installed.png", "multi-installed.png",
    "stale-invalidated.png", "rediscovered-b.png", "stale-recovered.png")) {
  $full = Join-Path $out $p
  if (Test-Path $full) { Remove-Item -Recurse -Force $full }
}
$log = New-Object System.Collections.Generic.List[string]
function Log([string]$s) { $line = "$s"; Write-Output $line; $log.Add($line) }

# ---- 1) 造两个源仓库（不联网，经 insteadOf 冒充 github 地址）----
# 仓库 A（$RepoName）：3 个技能，其中一个的 frontmatter name 与目录名不同；
# 仓库 B（$RepoName2）：另外 2 个技能，用于验证「改了仓库后旧候选失效」。
$home2 = Join-Path $out "home"
New-Item -ItemType Directory -Force -Path $home2 | Out-Null
$gitcfg = Join-Path $out "gitconfig"
# 写文件不带 BOM：带 BOM 时 SKILL.md 的第一行会变成 "\ufeff---"，
# frontmatter 就认不出来了（这次的验证里踩到过）。
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
function Write-Text([string]$path, [string]$text) {
  [System.IO.File]::WriteAllText($path, $text, $utf8NoBom)
}
$skills = @(
  @{ dir = "skills/engineering/tdd"; name = "test-driven-development" },
  @{ dir = "skills/engineering/code-review"; name = "code-review" },
  @{ dir = "skills/productivity/grill-me"; name = "grill-me" }
)
$skills2 = @(
  @{ dir = "skills/gamma"; name = "gamma" },
  @{ dir = "skills/delta"; name = "delta" }
)
# New-SkillRepo 在 path 下造一个含给定技能的 git 仓库，并用 insteadOf 把
# $alias 的 github 地址重写到它的 bare 镜像。
function New-SkillRepo([string]$path, $items, [string]$alias) {
  foreach ($s in $items) {
    $d = Join-Path $path ($s.dir -replace "/", "\")
    New-Item -ItemType Directory -Force -Path $d | Out-Null
    Write-Text (Join-Path $d "SKILL.md") "---`nname: $($s.name)`n---`n`n# $($s.name)`n"
  }
  Write-Text (Join-Path $path "README.md") "# 验证用仓库 $alias`n"
  git -C $path init -q
  git -C $path config user.email t@t
  git -C $path config user.name t
  git -C $path add -A
  git -C $path commit -q -m init
  $bare = Join-Path $path "bare.git"
  git clone -q --bare $path $bare
  $bareSlash = $bare -replace "\\", "/"
  git config --global "url.$bareSlash.insteadOf" "https://github.com/$alias.git"
  return $bare
}

# 隔离的 git 全局配置：屏蔽本机配置，两条 insteadOf 规则都写进同一份文件。
"" | Set-Content -Path $gitcfg -Encoding ascii
$env:GIT_CONFIG_GLOBAL = $gitcfg
$env:GIT_CONFIG_SYSTEM = $gitcfg
$env:GIT_CONFIG_NOSYSTEM = "1"
$repo = Join-Path $out "src-repo"
$bare = New-SkillRepo $repo $skills $RepoName
$repo2 = Join-Path $out "src-repo2"
$bare2 = New-SkillRepo $repo2 $skills2 $RepoName2
Log "SETUP repo=$repo bare=$bare repo2=$repo2 bare2=$bare2 gitconfig=$gitcfg home=$home2"

# ---- 2) 起进程（隔离 HOME） ----
$env:USERPROFILE = $home2
$env:HOME = $home2
$proc = Start-Process -FilePath $Exe -PassThru
Start-Sleep -Milliseconds $WaitMs
$h = $proc.MainWindowHandle
$deadline = (Get-Date).AddSeconds(30)
while (($h -eq 0 -or -not [Win]::IsWindowVisible($h)) -and (Get-Date) -lt $deadline) {
  Start-Sleep -Milliseconds 500
  $proc.Refresh()
  $h = $proc.MainWindowHandle
}
if ($h -eq 0) { Log "NO_WINDOW"; if (-not $proc.HasExited) { $proc.Kill() }; exit 1 }
[Win]::MoveWindow($h, 60, 60, 1200, 820, $true) | Out-Null
Start-Sleep -Milliseconds 1200
$r = New-Object Win+RECT
[Win]::GetWindowRect($h, [ref]$r) | Out-Null
Log "RECT $($r.Left),$($r.Top) $($r.Right - $r.Left)x$($r.Bottom - $r.Top) pid=$($proc.Id)"

# ---- UIA 工具 ----
$uiaRoot = [System.Windows.Automation.AutomationElement]::FromHandle($h)
function Find-All([string]$name) {
  $cond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, $name)
  return $uiaRoot.FindAll([System.Windows.Automation.TreeScope]::Descendants, $cond)
}
function Find-With([string]$name, $pattern) {
  foreach ($e in (Find-All $name)) {
    $p = $null
    if ($e.TryGetCurrentPattern($pattern, [ref]$p)) { return @{ el = $e; pattern = $p } }
  }
  return $null
}
function Invoke-Named([string]$name) {
  $hit = Find-With $name ([System.Windows.Automation.InvokePattern]::Pattern)
  if ($null -eq $hit) { throw "找不到可点击的元素：$name" }
  $hit.pattern.Invoke()
  Log "CLICK $name"
  Start-Sleep -Milliseconds 500
}
# Toggle-Named 切换复选框并校验「真的生效」：偶发过一次 UIA 的单次
# Toggle() 被应用了两次（状态又弹回去），所以这里等一拍后读回真实状态，
# 不对就重试（最多 3 次）。
function Toggle-Named([string]$name) {
  for ($try = 1; $try -le 3; $try++) {
    $hit = Find-With $name ([System.Windows.Automation.TogglePattern]::Pattern)
    if ($null -eq $hit) { throw "找不到复选框：$name" }
    $was = "$($hit.pattern.Current.ToggleState)"
    $want = if ($was -eq "On") { "Off" } else { "On" }
    $hit.pattern.Toggle()
    Start-Sleep -Milliseconds 600
    $back = Find-With $name ([System.Windows.Automation.TogglePattern]::Pattern)
    if ($null -eq $back) { throw "切换后复选框消失：$name" }
    $now = "$($back.pattern.Current.ToggleState)"
    if ($now -eq $want) { Log "TOGGLE $name $was -> $now ok"; return }
    Log "TOGGLE $name $was -> $now（期望 $want）第 $try 次未生效，重试"
  }
  throw "切换复选框失败：$name"
}
function Set-Named([string]$name, [string]$value) {
  $hit = Find-With $name ([System.Windows.Automation.ValuePattern]::Pattern)
  if ($null -eq $hit) { throw "找不到输入框：$name" }
  $hit.pattern.SetValue($value)
  Log "TYPE $name = $value"
  Start-Sleep -Milliseconds 600
}
# Count-Named-Contains 统计无障碍树里名字包含 needle 的元素个数（错误文案是长
# 字符串，无法用 Find-All 的精确匹配）。
function Count-Named-Contains([string]$needle) {
  $all = $uiaRoot.FindAll([System.Windows.Automation.TreeScope]::Descendants,
    [System.Windows.Automation.Condition]::TrueCondition)
  $n = 0
  foreach ($e in $all) { if ($e.Current.Name -like "*$needle*") { $n++ } }
  return $n
}
function Wait-Named([string]$name, [int]$seconds = 60) {
  $until = (Get-Date).AddSeconds($seconds)
  while ((Get-Date) -lt $until) {
    $el = $uiaRoot.FindFirst([System.Windows.Automation.TreeScope]::Descendants,
      (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, $name)))
    if ($null -ne $el) { return $el }
    Start-Sleep -Milliseconds 400
  }
  throw "等待超时：$name"
}
function Save-Shot([string]$file) {
  $rr = New-Object Win+RECT
  [Win]::GetWindowRect($h, [ref]$rr) | Out-Null
  $w = $rr.Right - $rr.Left; $ht = $rr.Bottom - $rr.Top
  $bmp = New-Object System.Drawing.Bitmap($w, $ht)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $hdc = $g.GetHdc()
  $ok = [Win]::PrintWindow($h, $hdc, 2)
  $g.ReleaseHdc($hdc); $g.Dispose()
  $bmp.Save($file, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
  Log "SAVED $file PRINTWINDOW=$ok"
}
function Dump-Tree() {
  $all = $uiaRoot.FindAll([System.Windows.Automation.TreeScope]::Descendants,
    [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($e in $all) {
    $pats = @()
    foreach ($pair in @(
        @("Invoke", [System.Windows.Automation.InvokePattern]::Pattern),
        @("Toggle", [System.Windows.Automation.TogglePattern]::Pattern),
        @("Value", [System.Windows.Automation.ValuePattern]::Pattern))) {
      $p = $null
      if ($e.TryGetCurrentPattern($pair[1], [ref]$p)) { $pats += $pair[0] }
    }
    Log ("NODE type={0} name={1} patterns={2}" -f $e.Current.ControlType.ProgrammaticName, $e.Current.Name, ($pats -join ","))
  }
}

try {
  # ---- 3) 打开安装弹窗、填仓库、发现候选 ----
  Invoke-Named "安装技能"
  Set-Named "仓库" $RepoName
  Invoke-Named "确认安装"
  Wait-Named "skills/engineering/tdd" 90 | Out-Null
  Log "FOUND candidates"
  if ($Dump) {
    Dump-Tree
    Save-Shot (Join-Path $out "dump.png")
    throw "DUMP_ONLY"
  }
  # 候选列表里的复选框与勾选态（文字证据）。
  foreach ($s in $skills) {
    $hit = Find-With $s.dir ([System.Windows.Automation.TogglePattern]::Pattern)
    if ($null -eq $hit) { Log "MISSING checkbox $($s.dir)" }
    else { Log "CAND $($s.dir) toggle=$($hit.pattern.Current.ToggleState)" }
  }
  # 勾选一个候选：主按钮应从「安装选中的技能」变成「安装选中的 1 个」。
  # （Toggle-Named 自己会校验这一次切换真的生效，见它的注释。）
  Toggle-Named "skills/engineering/tdd"
  Save-Shot (Join-Path $out "candidates-1-checked.png")

  # ---- 4) 一次装完勾选的技能（真实走 store.AddMany） ----
  Invoke-Named "确认安装"
  Start-Sleep -Seconds 4
  Save-Shot (Join-Path $out "installed.png")

  # ---- 5) 第二轮：勾两个候选，一次装完（多选安装的真实窗口验证） ----
  Invoke-Named "安装技能"
  Set-Named "仓库" $RepoName
  Invoke-Named "确认安装"
  Wait-Named "skills/engineering/code-review" 90 | Out-Null
  Toggle-Named "skills/engineering/code-review"
  Toggle-Named "skills/productivity/grill-me"
  Save-Shot (Join-Path $out "candidates-2-checked.png")
  Invoke-Named "确认安装"
  Start-Sleep -Seconds 4
  Save-Shot (Join-Path $out "multi-installed.png")

  # ---- 6) 回归（本次修复）：改「仓库」输入框后旧候选必须失效 ----
  # 6a) 重新用仓库 A 发现候选（A 的 3 个技能已经装上，但发现看的是仓库内容）。
  Invoke-Named "安装技能"
  Set-Named "仓库" $RepoName
  Invoke-Named "确认安装"
  Wait-Named "skills/engineering/tdd" 90 | Out-Null
  Toggle-Named "skills/engineering/tdd"
  Log "STALE-BEFORE repo=$RepoName"
  # 6b) 把仓库改成 B（不点安装）：候选应立刻失效、不再显示，主按钮回到「安装」。
  Set-Named "仓库" $RepoName2
  Save-Shot (Join-Path $out "stale-invalidated.png")
  $stale = Find-With "skills/engineering/tdd" ([System.Windows.Automation.TogglePattern]::Pattern)
  if ($null -eq $stale) { Log "STALE-HIDDEN ok" } else { Log "STALE-HIDDEN FAILED（旧候选还在：$($stale.el.Current.Name)）" }
  Log "STALE-HINT $(if ((Find-All '仓库或分支已改，点「安装」重新发现技能。').Count -gt 0) { 'shown' } else { 'MISSING' })"
  # 6c) 再点安装：按仓库 B 重新发现（不拿 A 的路径去装 B）。
  Invoke-Named "确认安装"
  Wait-Named "skills/delta" 90 | Out-Null
  Save-Shot (Join-Path $out "rediscovered-b.png")
  $err = Count-Named-Contains "仓库内不存在目录"
  Log "STALE-ERR-COUNT $err（应为 0：不该拿 A 的路径去装 B）"
  # 6d) 装 B 的一个技能，确认后续走的就是新仓库。
  Toggle-Named "skills/gamma"
  Invoke-Named "确认安装"
  Start-Sleep -Seconds 4
  Save-Shot (Join-Path $out "stale-recovered.png")
  Log "SKILLS-ON-DISK"
  foreach ($d in (Get-ChildItem (Join-Path $home2 ".agents\skills") -Directory)) { Log ("  " + $d.Name) }
} finally {
  if (-not $proc.HasExited) { $proc.Kill() }
  Log "CLOSED"
  $log | Set-Content -Path (Join-Path $out "evidence.txt") -Encoding utf8
}
