# kura 启动并按窗口截图（真机验证用）。用法：
#   powershell -NoProfile -ExecutionPolicy Bypass -File capture-window.ps1 -Exe <exe 路径> -Out <png 路径>
#
# 截图用 PrintWindow(PW_RENDERFULLCONTENT)，直接向窗口要一帧，不依赖前台
# 焦点——用全屏 CopyFromScreen 时别的窗口（浏览器等）一抢前台就会截到它。
# 窗口按 RECT 抓，右侧/下侧会带进一点 desktop 阴影，脚本把四条边各内缩 8px
# 去掉它。
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [Parameter(Mandatory=$true)][string]$Out,
  [string]$TitleLike = "kura",
  [int]$WaitMs = 9000,
  [int]$Inset = 8
)

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Win {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);
  [DllImport("shcore.dll")] public static extern int SetProcessDpiAwareness(int v);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
}
"@

try { [Win]::SetProcessDpiAwareness(2) | Out-Null } catch {}

$proc = Start-Process -FilePath $Exe -PassThru
Start-Sleep -Milliseconds $WaitMs

$h = $proc.MainWindowHandle
$deadline = (Get-Date).AddSeconds(20)
while (($h -eq 0 -or -not [Win]::IsWindowVisible($h)) -and (Get-Date) -lt $deadline) {
  Start-Sleep -Milliseconds 500
  $proc.Refresh()
  $h = $proc.MainWindowHandle
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
