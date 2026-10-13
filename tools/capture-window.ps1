# Launch kura, capture its window, and close it. Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File capture-window.ps1 -Exe <path> -Out <png>
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [Parameter(Mandatory=$true)][string]$Out,
  [string]$TitleLike = "kura",
  [int]$WaitMs = 9000
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

[Win]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds 1200

$r = New-Object Win+RECT
[Win]::GetWindowRect($h, [ref]$r) | Out-Null
$w = $r.Right - $r.Left
$ht = $r.Bottom - $r.Top
Write-Output "RECT $($r.Left),$($r.Top) ${w}x${ht}"

$bmp = New-Object System.Drawing.Bitmap($w, $ht)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
$g.Dispose()
$bmp.Save($Out, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Dispose()
Write-Output "SAVED $Out"

if (-not $proc.HasExited) { $proc.Kill() }
Write-Output "CLOSED"
