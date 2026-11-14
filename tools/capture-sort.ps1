# 技能排序（按修改时间 / 按名称）的真机验证（背景见 REQUIREMENTS.md 的「Sort order」）。用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-sort.ps1 -Exe <kura.exe 绝对路径> [-OutDir <截图目录>]
#
# 全程在隔离环境里进行，绝不碰真实的 ~/.agents、~/.kura：
#   - USERPROFILE / HOME / APPDATA / LOCALAPPDATA / TEMP / TMP 全部指向一个新建的临时目录；
#   - 默认技能根 ~/.agents/skills 里放 8 个技能，用 SetLastWriteTime 把每个技能的 SKILL.md 与目录自身的
#     mtime 摆成已知时刻（名称顺序、新的在前、旧的在前三种顺序两两不同），目录最后设；
#   - 每个场景写一份 ~/.kura/config.json（sort / theme / view；lang 固定 zh；autoCheckUpdates=false，
#     截图期间应用不做后台联网的更新检查），交给 capture-window.ps1 启动、截图、关闭；
#   - 结束后核对技能的 mtime 没被应用改动，再把临时目录删干净。
#
# 输出：每个场景的 EXPECT 行（预期的前几个技能名与完整顺序），capture-window.ps1 的 RECT / SAVED / CLOSED 行，
# 以及 MTIME 核对与 CLEANED 行。截图是临时材料（已被 .gitignore 忽略），验证后不入库。
# -KeepWork 保留临时目录，便于排查（用完请手动删）。
# -Only "name-light,oldest-dark" 只跑指定名字的场景（逗号分隔），重拍某一张时用。
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [string]$OutDir = (Join-Path $PSScriptRoot "sort-window"),
  [int]$WaitMs = 9000,
  [string]$Only = "",
  [int]$MaxAttempts = 3,
  [switch]$KeepWork
)

$ErrorActionPreference = "Stop"
$Exe = (Resolve-Path -LiteralPath $Exe).Path
$OutDir = [IO.Path]::GetFullPath($OutDir)
if (-not (Test-Path -LiteralPath $OutDir)) { New-Item -ItemType Directory -Path $OutDir | Out-Null }

$work = Join-Path ([IO.Path]::GetTempPath()) ("kura-sort-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$userHome = Join-Path $work "home"
$skills = Join-Path $userHome ".agents\skills"
$kura = Join-Path $userHome ".kura"
$tmp = Join-Path $work "tmp"

# 8 个技能：名字 -> 相对基准的小时数（mtime）。三种顺序：
#   名称：      api-design brainstorm code-review debug-trace pdf-tools release-notes tdd web-search
#   新的在前：  web-search brainstorm pdf-tools code-review release-notes api-design tdd debug-trace
#   旧的在前：  debug-trace tdd api-design release-notes code-review pdf-tools brainstorm web-search
$hours = [ordered]@{
  "api-design" = 12; "brainstorm" = 90; "code-review" = 55; "debug-trace" = 3
  "pdf-tools" = 70; "release-notes" = 31; "tdd" = 8; "web-search" = 120
}
$byName = @($hours.Keys | Sort-Object)
$newest = @($hours.Keys | Sort-Object { $hours[$_] } -Descending)
$oldest = @($hours.Keys | Sort-Object { $hours[$_] })

# 场景：排序偏好（原始 JSON，$null 表示 config.json 里没有 sort 一项）、外观、排列、预期顺序。
$scenarios = @(
  @{ Name = "default-light";      Sort = $null;                          Theme = "light"; View = "grid"; Expect = $newest },
  @{ Name = "newest-light";       Sort = '{"key":"time","desc":true}';   Theme = "light"; View = "grid"; Expect = $newest },
  @{ Name = "newest-dark";        Sort = '{"key":"time","desc":true}';   Theme = "dark";  View = "grid"; Expect = $newest },
  @{ Name = "oldest-light";       Sort = '{"key":"time","desc":false}';  Theme = "light"; View = "grid"; Expect = $oldest },
  @{ Name = "oldest-dark";        Sort = '{"key":"time","desc":false}';  Theme = "dark";  View = "grid"; Expect = $oldest },
  @{ Name = "name-light";         Sort = '{"key":"name"}';               Theme = "light"; View = "grid"; Expect = $byName },
  @{ Name = "name-dark";          Sort = '{"key":"name"}';               Theme = "dark";  View = "grid"; Expect = $byName },
  @{ Name = "broken-sort-dark";   Sort = '"by-vibes"';                   Theme = "dark";  View = "grid"; Expect = $newest },
  @{ Name = "list-oldest-light";  Sort = '{"key":"time","desc":false}';  Theme = "light"; View = "list"; Expect = $oldest }
)

if ($Only -ne "") {
  $wanted = @($Only -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" })
  $scenarios = @($scenarios | Where-Object { $wanted -contains $_.Name })
  if ($scenarios.Count -eq 0) { throw "no scenario matches -Only '$Only'" }
}

function New-Skill([string]$name, [int]$h) {
  $dir = Join-Path $skills $name
  New-Item -ItemType Directory -Path $dir -Force | Out-Null
  $body = "---`nname: $name`ndescription: Description of $name.`n---`n`n# $name`n"
  $md = Join-Path $dir "SKILL.md"
  [IO.File]::WriteAllText($md, $body, (New-Object Text.UTF8Encoding($false)))
  $t = $script:base.AddHours($h)
  [IO.File]::SetLastWriteTime($md, $t)
  [IO.Directory]::SetLastWriteTime($dir, $t)   # 目录最后设：往里写文件会把目录的 mtime 改回现在
}

function Get-Mtimes {
  $r = [ordered]@{}
  foreach ($k in $hours.Keys) {
    $dir = Join-Path $skills $k
    $r[$k] = "{0:o}|{1:o}" -f (Get-Item -LiteralPath $dir).LastWriteTimeUtc, (Get-Item -LiteralPath (Join-Path $dir "SKILL.md")).LastWriteTimeUtc
  }
  return $r
}

# 保存环境变量，结束时还原（本脚本在调用者的进程里改它们）。
$saved = @{}
foreach ($k in "USERPROFILE", "HOME", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP") { $saved[$k] = [Environment]::GetEnvironmentVariable($k, "Process") }

try {
  foreach ($d in $userHome, $skills, $kura, $tmp, (Join-Path $userHome "AppData\Roaming"), (Join-Path $userHome "AppData\Local")) {
    New-Item -ItemType Directory -Path $d -Force | Out-Null
  }
  $script:base = [datetime]::new(2025, 3, 5, 6, 7, 8, [DateTimeKind]::Local)
  foreach ($k in $hours.Keys) { New-Skill $k $hours[$k] }
  $before = Get-Mtimes

  Write-Output ("LAYOUT home={0}" -f $userHome)
  foreach ($k in $hours.Keys) { Write-Output ("LAYOUT skill {0,-14} +{1,3}h  {2}" -f $k, $hours[$k], $before[$k]) }

  # 隔离：子进程继承这些环境变量。
  $env:USERPROFILE = $userHome
  $env:HOME = $userHome
  $env:APPDATA = Join-Path $userHome "AppData\Roaming"
  $env:LOCALAPPDATA = Join-Path $userHome "AppData\Local"
  $env:TEMP = $tmp
  $env:TMP = $tmp

  foreach ($s in $scenarios) {
    $sortPart = ""
    if ($null -ne $s.Sort) { $sortPart = ',"sort":' + $s.Sort }
    $json = '{"roots":[],"theme":"' + $s.Theme + '","view":"' + $s.View + '","lang":"zh","autoCheckUpdates":false' + $sortPart + '}'
    $cfgPath = Join-Path $kura "config.json"
    $png = Join-Path $OutDir ($s.Name + ".png")
    Write-Output ""
    Write-Output ("SCENARIO {0}: config.json = {1}" -f $s.Name, $json)
    Write-Output ("EXPECT {0}: first 3 = {1} ; full order = {2}" -f $s.Name, (($s.Expect | Select-Object -First 3) -join ", "), ($s.Expect -join ", "))
    # 截图期间窗口会在桌面最上面十来秒，这台机器如果正在被人用，鼠标点击可能落到窗口里的控件上。
    # 应用在不被点击时不会改写 config.json（实测过：逐字节相同），而点了主题/排列/排序开关会立刻落盘，
    # 所以截图后 config.json 变了就说明窗口被点过：这张图作废，重拍。
    $attempt = 0
    while ($true) {
      $attempt++
      [IO.File]::WriteAllText($cfgPath, $json, (New-Object Text.UTF8Encoding($false)))
      & (Join-Path $PSScriptRoot "capture-window.ps1") -Exe $Exe -Out $png -WaitMs $WaitMs
      $found = [IO.File]::ReadAllText($cfgPath)
      if ($found -eq $json) {
        Write-Output ("CONFIG {0}: unchanged by the app (attempt {1}), so nothing clicked the window" -f $s.Name, $attempt)
        break
      }
      Write-Output ("RETRY {0}: config.json was rewritten while the window was open (attempt {1}/{2}), something clicked a control;`n    wrote: {3}`n    found: {4}" -f $s.Name, $attempt, $MaxAttempts, $json, $found)
      if ($attempt -ge $MaxAttempts) {
        Write-Output ("GAVE UP {0}: still disturbed after {1} attempts, do NOT use this screenshot" -f $s.Name, $attempt)
        break
      }
    }
  }

  # 应用不该改动技能的 mtime（它只读技能目录）；改了的话顺序证据就不可信。
  $after = Get-Mtimes
  $same = $true
  foreach ($k in $hours.Keys) { if ($before[$k] -ne $after[$k]) { $same = $false; Write-Output ("MTIME CHANGED {0}: {1} -> {2}" -f $k, $before[$k], $after[$k]) } }
  Write-Output ("MTIME unchanged by the app: {0}" -f $same)
}
finally {
  foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k], "Process") }
  if (-not $KeepWork -and (Test-Path -LiteralPath $work)) {
    # 应用刚被杀掉，显卡驱动写在隔离 LOCALAPPDATA 下的着色器缓存（NVIDIA\DXCache）可能还占着几秒，所以重试删除，最多 10 秒。
    for ($i = 0; $i -lt 20 -and (Test-Path -LiteralPath $work); $i++) {
      Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue
      if (Test-Path -LiteralPath $work) { Start-Sleep -Milliseconds 500 }
    }
    Write-Output ("CLEANED work dir removed: {0}" -f (-not (Test-Path -LiteralPath $work)))
  } elseif ($KeepWork) {
    Write-Output ("KEPT work dir: {0}" -f $work)
  }
}
