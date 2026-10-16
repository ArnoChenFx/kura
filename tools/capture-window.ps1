# kura 启动并按窗口截图（真机验证用）。用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File capture-window.ps1 -Exe <exe 路径> -Out <png 路径>
#
# 截图用 PrintWindow(PW_RENDERFULLCONTENT)，直接向窗口要一帧，不依赖前台
# 焦点——用全屏 CopyFromScreen 时别的窗口（浏览器等）一抢前台就会截到它。
# 窗口按 RECT 抓，右侧/下侧会带进一点 desktop 阴影，脚本把四条边各内缩 8px
# 去掉它。
#
# -LargestWindow 改成「按面积挑本进程所有可见顶层窗口中最大的那个」：应用的
# 某个功能弹出自己的窗口时（如 kura 更新插件那个「不抢焦点」的更新窗口），
# Windows 会把 MainWindowHandle 换到新窗口上，默认做法就会截到它而不是主窗口。
# 默认（不加这个开关）行为不变，仍用 MainWindowHandle。
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [Parameter(Mandatory=$true)][string]$Out,
  [string]$TitleLike = "kura",
  [int]$WaitMs = 9000,
  [int]$Inset = 8,
  [switch]$LargestWindow,
  [int]$MinArea = 1200000
)

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public class Win {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);
  [DllImport("shcore.dll")] public static extern int SetProcessDpiAwareness(int v);
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc fn, IntPtr l);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
  // visibleWindowsOf 列出该进程的全部可见顶层窗口。
  public static List<IntPtr> visibleWindowsOf(uint pid) {
    var list = new List<IntPtr>();
    EnumWindows((h, l) => {
      uint p; GetWindowThreadProcessId(h, out p);
      if (p == pid && IsWindowVisible(h)) list.Add(h);
      return true;
    }, IntPtr.Zero);
    return list;
  }
}
"@

try { [Win]::SetProcessDpiAwareness(2) | Out-Null } catch {}

# largestVisible 返回本进程可见顶层窗口里面积最大的那个的句柄与面积；
# 没有达到 minArea 的窗口就返回空（等下一轮），免得挑到刚弹出的小窗。
function Get-LargestVisible($proc, [int]$minArea) {
  $best = [IntPtr]::Zero; $bestArea = 0
  foreach ($p in (Get-Process -Name $proc.ProcessName -ErrorAction SilentlyContinue)) {
    foreach ($hh in [Win]::visibleWindowsOf([uint32]$p.Id)) {
      $rr = New-Object Win+RECT
      [Win]::GetWindowRect($hh, [ref]$rr) | Out-Null
      $area = ($rr.Right - $rr.Left) * ($rr.Bottom - $rr.Top)
      if ($area -gt $bestArea) { $bestArea = $area; $best = $hh }
    }
  }
  if ($bestArea -ge $minArea) { return @{ handle = $best; area = $bestArea } }
  return $null
}

$proc = Start-Process -FilePath $Exe -PassThru
Start-Sleep -Milliseconds $WaitMs

$h = [IntPtr]::Zero
if ($LargestWindow) {
  $found = Get-LargestVisible $proc $MinArea
  if ($null -ne $found) { $h = $found.handle; Write-Output "LARGEST area=$($found.area)" }
} else {
  $h = $proc.MainWindowHandle
}
$deadline = (Get-Date).AddSeconds(20)
if ($LargestWindow) {
  while ($h -eq [IntPtr]::Zero -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    $found = Get-LargestVisible $proc $MinArea
    if ($null -ne $found) { $h = $found.handle }
  }
} else {
  while (($h -eq 0 -or -not [Win]::IsWindowVisible($h)) -and (Get-Date) -lt $deadline) {
    Start-Sleep -Milliseconds 500
    $proc.Refresh()
    $h = $proc.MainWindowHandle
  }
}

if ($h -eq 0) {
  Write-Output "NO_WINDOW"
  if (-not $proc.HasExited) { $proc.Kill() }
  exit 1
}

# 给一帧的时间渲染完（原生 UI 首帧可能只有背景）。
Start-Sleep -Milliseconds 1500

$r = New-Object Win+RECT
[Win]::GetWindowRect($h, [ref]$r) | Out-Null
$w = $r.Right - $r.Left
$ht = $r.Bottom - $r.Top
Write-Output "RECT $($r.Left),$($r.Top) ${w}x${ht}"

$bmp = New-Object System.Drawing.Bitmap($w, $ht)
$g = [System.Drawing.Graphics]::FromImage($bmp)
# 背景刷成窗口底色，避免 PrintWindow 没覆盖到的地方是黑的。
$g.Clear([System.Drawing.Color]::FromArgb(28, 28, 32))
$hdc = $g.GetHdc()
$ok = [Win]::PrintWindow($h, $hdc, 2)   # 2 = PW_RENDERFULLCONTENT
$g.ReleaseHdc($hdc)
$g.Dispose()
Write-Output "PRINTWINDOW=$ok"

# 内缩去掉 Windows 的窗口阴影带进来的桌面背景。
$iw = $w - 2 * $Inset
$ih = $ht - 2 * $Inset
if ($ok -and $iw -gt 0 -and $ih -gt 0) {
  $crop = New-Object System.Drawing.Bitmap($iw, $ih)
  $cg = [System.Drawing.Graphics]::FromImage($crop)
  $cg.DrawImage($bmp, (New-Object System.Drawing.Rectangle(0, 0, $iw, $ih)),
    (New-Object System.Drawing.Rectangle($Inset, $Inset, $iw, $ih)),
    [System.Drawing.GraphicsUnit]::Pixel)
  $cg.Dispose()
  $bmp.Dispose()
  $bmp = $crop
}

$bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Dispose()
Write-Output "SAVED $Out"

if (-not $proc.HasExited) { $proc.Kill() }
Write-Output "CLOSED"
