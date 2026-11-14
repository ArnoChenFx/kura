# 链接形式的技能 + 左栏只显示存在目录的真机验证（背景见 docs/note/linked-skills.md）。用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-links.ps1 -Exe <kura.exe 绝对路径> -Out <png 路径>
#
# 全程在隔离环境里进行，绝不碰真实的 ~/.agents、~/.kura：
#   - USERPROFILE / HOME / APPDATA / LOCALAPPDATA / TEMP / TMP 全部指向一个新建的临时目录；
#   - 默认技能根 ~/.agents/skills 里放 10 个真目录 + 3 个联接点（名字照用户实测的 cherry-browser、
#     cherry-computer、ego-browser）+ 1 个断链（不应显示）；
#   - ~/.kura/config.json 再记两个目录：一个存在（.claude/skills，2 个技能），一个不存在（左栏不应出现）。
# 然后交给 capture-window.ps1 启动、截图、关闭。结束后把临时目录删干净：联接点先用
# [IO.Directory]::Delete 只摘链接，再递归删其余，不会穿透到目标。
#
# 输出：capture-window.ps1 的 RECT / SAVED / CLOSED 行，以及本脚本摆好的布局（LAYOUT 行）。
# -KeepWork 保留临时目录，便于排查（用完请手动删）。
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [Parameter(Mandatory=$true)][string]$Out,
  [int]$WaitMs = 9000,
  [switch]$KeepWork
)

$ErrorActionPreference = "Stop"
$Exe = (Resolve-Path -LiteralPath $Exe).Path
$OutDir = Split-Path -Parent ([IO.Path]::GetFullPath($Out))
if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }

$work = Join-Path ([IO.Path]::GetTempPath()) ("kura-links-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$userHome = Join-Path $work "home"
$skills = Join-Path $userHome ".agents\skills"
$elsewhere = Join-Path $work "elsewhere"          # 联接点的真实目标（技能根之外）
$projects = Join-Path $work "projects"
$extra = Join-Path $projects ".claude\skills"      # 存在的额外根
$ghost = Join-Path $projects "gone\skills"         # 不存在的额外根（从不创建）
$tmp = Join-Path $work "tmp"

function New-Skill([string]$dir, [string]$name, [string]$desc) {
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  $body = "---`nname: $name`ndescription: $desc`n---`n`n# $name`n"
  [IO.File]::WriteAllText((Join-Path $dir "SKILL.md"), $body, (New-Object Text.UTF8Encoding($false)))
}

# 保存环境变量，结束时还原（本脚本在调用者的进程里改它们）。
$saved = @{}
foreach ($k in "USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP") { $saved[$k] = [Environment]::GetEnvironmentVariable($k, "Process") }

try {
  foreach ($d in $userHome, $skills, $elsewhere, $extra, $tmp, (Join-Path $userHome "AppData\Roaming"), (Join-Path $userHome "AppData\Local")) {
    New-Item -ItemType Directory -Path $d -Force | Out-Null
  }

  # 10 个真目录。
  1..10 | ForEach-Object {
    $n = "real-skill-{0:d2}" -f $_
    New-Skill (Join-Path $skills $n) $n "A plain skill folder number $_."
  }
  # 3 个联接点：真实内容在 elsewhere 里。
  foreach ($n in "cherry-browser", "cherry-computer", "ego-browser") {
    New-Skill (Join-Path $elsewhere $n) $n "Linked skill ${n}: a junction that points outside the skills folder."
    New-Item -ItemType Junction -Path (Join-Path $skills $n) -Target (Join-Path $elsewhere $n) | Out-Null
  }
  # 1 个断链：目标被删掉，链接还在。它不应出现在界面里，也不能让界面报错。
  $goneTarget = Join-Path $elsewhere "broken-target"
  New-Item -ItemType Directory -Path $goneTarget | Out-Null
  New-Item -ItemType Junction -Path (Join-Path $skills "broken-link") -Target $goneTarget | Out-Null
  Remove-Item -LiteralPath $goneTarget -Force

  # 存在的额外根里放 2 个技能。
  New-Skill (Join-Path $extra "project-helper") "project-helper" "A skill that belongs to a project."
  New-Skill (Join-Path $extra "project-notes") "project-notes" "Another project skill."

  # config.json：记着一个存在的和一个不存在的额外根；固定中文界面，截图可复现。
  $cfg = [ordered]@{ roots = @($extra, $ghost); lang = "zh" } | ConvertTo-Json
  $kura = Join-Path $userHome ".kura"
  New-Item -ItemType Directory -Path $kura -Force | Out-Null
  [IO.File]::WriteAllText((Join-Path $kura "config.json"), $cfg, (New-Object Text.UTF8Encoding($false)))

  Write-Output ("LAYOUT home={0}" -f $userHome)
  Write-Output ("LAYOUT default root {0}: {1} entries (10 real dirs + 3 junctions + 1 broken junction)" -f $skills, (Get-ChildItem -LiteralPath $skills -Force).Count)
  Write-Output ("LAYOUT extra root {0}: exists, 2 skills" -f $extra)
  Write-Output ("LAYOUT ghost root {0}: does NOT exist (must not be listed)" -f $ghost)

  # 隔离：子进程继承这些环境变量。
  $env:USERPROFILE = $userHome
  $env:HOME = $userHome
  $env:APPDATA = Join-Path $userHome "AppData\Roaming"
  $env:LOCALAPPDATA = Join-Path $userHome "AppData\Local"
  $env:TEMP = $tmp
  $env:TMP = $tmp

  & (Join-Path $PSScriptRoot "capture-window.ps1") -Exe $Exe -Out $Out -WaitMs $WaitMs
}
finally {
  foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], "Process") }
  if (-not $KeepWork -and (Test-Path -LiteralPath $work)) {
    # 先只摘掉所有联接点（非递归 Delete 对联接点只删链接本身），再递归删除其余内容。
    Get-ChildItem -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue |
      Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } |
      ForEach-Object { try { [IO.Directory]::Delete($_.FullName) } catch {} }
    Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output ("CLEANED work dir removed: {0}" -f (-not (Test-Path -LiteralPath $work)))
  } elseif ($KeepWork) {
    Write-Output ("KEPT work dir: {0}" -f $work)
  }
}
