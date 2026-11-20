# win_bloqueo.ps1 — execútao o minion (como SYSTEM) vía `cmd.script` desde
# bloqueoTotal.sls.  O minion está na sesión 0 e non pode debuxar na pantalla
# do alumno, así que aquí só se PROGRAMA unha tarefa ("PiztuBloqueo") que corre
# NA sesión do usuario conectado e fai o traballo real:
#   · overlay a pantalla completa (unha soa vez, estático)
#   · bloqueo de rato + teclado  (hooks WH_KEYBOARD_LL/WH_MOUSE_LL + BlockInput)
#   · silenciar o son            (Core Audio, SetMute no endpoint por defecto)
# Todo se mantén ata que desaparece  %ProgramData%\piztu_bloqueo.flag
# (o que fai win_desbloqueo.ps1).  Universal: Win7→11, calquera edición.
$ProgressPreference = 'SilentlyContinue'

# Usuario da sesion de consola ACTIVA. NON vale coller o primeiro explorer.exe
# que apareza: se hai varias sesions abertas a vez (tipico de "Cambiar de
# usuario" sen pechar a anterior), o bloqueo iriase a sesion equivocada,
# invisible para quen esta de verdade diante da pantalla.
try {
  Add-Type -Name Wts -Namespace Piztu -MemberDefinition '[DllImport("kernel32.dll")] public static extern uint WTSGetActiveConsoleSessionId();'
} catch {}
try { $sid = [int][Piztu.Wts]::WTSGetActiveConsoleSessionId() } catch { $sid = -1 }

$proc = Get-Process explorer -IncludeUserName -ErrorAction SilentlyContinue |
        Where-Object { $_.SessionId -eq $sid } | Select-Object -First 1
if (-not $proc) {
  $proc = Get-Process explorer -IncludeUserName -ErrorAction SilentlyContinue |
          Select-Object -First 1
}
$u = $proc.UserName
if (-not $u) { 'ninguen conectado'; return }

$flag = Join-Path $env:ProgramData 'piztu_bloqueo.flag'
$ovl  = Join-Path $env:ProgramData 'piztu_bloqueo_overlay.ps1'

# Restos dun desbloqueo anterior + bandeira nova.
schtasks /delete /tn PiztuDesbloqueo /f 2>$null | Out-Null
New-Item -ItemType File -Force -Path $flag | Out-Null

# ── Script que corre NA sesión do alumno ────────────────────────────────────
$overlay = @'
$ErrorActionPreference = 'SilentlyContinue'
$flag = Join-Path $env:ProgramData 'piztu_bloqueo.flag'

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type -Name Native -Namespace Piztu -MemberDefinition '[DllImport("user32.dll")] public static extern bool BlockInput(bool f);'
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
[Guid("5CDF2C82-841E-4546-9722-0CF74078229A"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAEV {
  int NotImpl1(); int NotImpl2();
  int GetChannelCount(out uint c);
  int SetMasterVolumeLevel(float l, ref Guid ctx);
  int SetMasterVolumeLevelScalar(float l, ref Guid ctx);
  int GetMasterVolumeLevel(out float l);
  int GetMasterVolumeLevelScalar(out float l);
  int SetChannelVolumeLevel(uint i, float l, ref Guid c);
  int SetChannelVolumeLevelScalar(uint i, float l, ref Guid c);
  int GetChannelVolumeLevel(uint i, out float l);
  int GetChannelVolumeLevelScalar(uint i, out float l);
  int SetMute([MarshalAs(UnmanagedType.Bool)] bool m, ref Guid c);
  int GetMute(out bool m);
}
[Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMD { int Activate(ref Guid iid, int ctx, IntPtr p, [MarshalAs(UnmanagedType.IUnknown)] out object o); }
[Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDE { int NotImpl(); int GetDefaultAudioEndpoint(int flow, int role, out IMMD dev); }
[ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")] class MMDE { }
public static class Snd {
  public static void Mute(bool m) {
    var e = (IMMDE)(new MMDE());
    IMMD d; e.GetDefaultAudioEndpoint(0, 1, out d);
    Guid iid = typeof(IAEV).GUID; object o;
    d.Activate(ref iid, 1, IntPtr.Zero, out o);
    Guid z = Guid.Empty;
    ((IAEV)o).SetMute(m, ref z);
  }
}
// Bloqueo real de rato + teclado sen elevacion: hooks de baixo nivel que
// tragan todo (HC_ACTION => devolver 1, non chamar CallNextHookEx). Non
// cobre Ctrl+Alt+Del (imposible desde modo usuario); si todo o demais.
public class Lock {
  const int WH_KEYBOARD_LL = 13, WH_MOUSE_LL = 14;
  public delegate IntPtr Proc(int code, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] static extern IntPtr SetWindowsHookEx(int id, Proc fn, IntPtr mod, uint tid);
  [DllImport("user32.dll")] static extern bool UnhookWindowsHookEx(IntPtr h);
  [DllImport("user32.dll")] static extern IntPtr CallNextHookEx(IntPtr h, int code, IntPtr w, IntPtr l);
  [DllImport("kernel32.dll")] static extern IntPtr GetModuleHandle(string n);
  static IntPtr hk = IntPtr.Zero, hm = IntPtr.Zero;
  static Proc pk, pm;
  static IntPtr Eat(int code, IntPtr w, IntPtr l) {
    if (code >= 0) return (IntPtr)1;
    return CallNextHookEx(IntPtr.Zero, code, w, l);
  }
  public static void On() {
    if (hk != IntPtr.Zero) return;
    pk = Eat; pm = Eat;
    IntPtr mod = GetModuleHandle(null);
    hk = SetWindowsHookEx(WH_KEYBOARD_LL, pk, mod, 0);
    hm = SetWindowsHookEx(WH_MOUSE_LL, pm, mod, 0);
  }
  public static void Off() {
    if (hk != IntPtr.Zero) { UnhookWindowsHookEx(hk); hk = IntPtr.Zero; }
    if (hm != IntPtr.Zero) { UnhookWindowsHookEx(hm); hm = IntPtr.Zero; }
  }
}
"@

try { [Snd]::Mute($true) } catch {}
try { [Piztu.Native]::BlockInput($true) | Out-Null } catch {}
try { [Lock]::On() } catch {}

$f = New-Object System.Windows.Forms.Form
$f.FormBorderStyle = 'None'
$f.WindowState     = 'Maximized'
$f.TopMost         = $true
$f.ShowInTaskbar   = $false
$f.BackColor       = [System.Drawing.Color]::FromArgb(26, 26, 46)
$f.Cursor          = [System.Windows.Forms.Cursors]::None
$l = New-Object System.Windows.Forms.Label
$l.Dock      = 'Fill'
$l.TextAlign = 'MiddleCenter'
$l.ForeColor = [System.Drawing.Color]::White
$l.Font      = New-Object System.Drawing.Font('Segoe UI', 36, [System.Drawing.FontStyle]::Bold)
$l.Text      = "EQUIPO BLOQUEADO`r`n`r`nAtende as indicacions do profesor"
$f.Controls.Add($l)
$f.Add_FormClosing({ param($s, $e) if (Test-Path $flag) { $e.Cancel = $true } })
$f.Add_Deactivate({ $f.TopMost = $true; try { $f.Activate() } catch {} })

$t = New-Object System.Windows.Forms.Timer
$t.Interval = 1000
$t.Add_Tick({
  if (-not (Test-Path $flag)) {
    $t.Stop()
    try { [Lock]::Off() } catch {}
    try { [Piztu.Native]::BlockInput($false) | Out-Null } catch {}
    try { [Snd]::Mute($false) } catch {}
    $f.Close()
  }
})
$t.Start()
[void]$f.ShowDialog()
try { [Lock]::Off() } catch {}
try { [Piztu.Native]::BlockInput($false) | Out-Null } catch {}
try { [Snd]::Mute($false) } catch {}
'@
Set-Content -Path $ovl -Value $overlay -Encoding UTF8

$tr = 'powershell -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' + $ovl
schtasks /create /tn PiztuBloqueo /f /sc ONCE /st 23:59 /it /ru $u /tr $tr | Out-Null
schtasks /run /tn PiztuBloqueo | Out-Null
"bloqueo lanzado para $u"
