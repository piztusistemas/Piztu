# win_desbloqueo.ps1 — execútao o minion (como SYSTEM) vía `cmd.script` desde
# desbloquearAula.sls.  Borra a bandeira (o overlay do alumno detéctao e
# libera rato/teclado + son e péchase só), elimina as tarefas e as regras de
# firewall, e programa un aviso de ~3 s ("PiztuDesbloqueo") na sesión do alumno.
$ProgressPreference = 'SilentlyContinue'

Remove-Item (Join-Path $env:ProgramData 'piztu_bloqueo.flag') -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 1500

schtasks /end    /tn PiztuBloqueo    2>$null | Out-Null
schtasks /delete /tn PiztuBloqueo /f 2>$null | Out-Null
netsh advfirewall firewall delete rule name=PIZTU_BLOQUEO     | Out-Null
netsh advfirewall firewall delete rule name=PIZTU_BLOQUEO_SSH | Out-Null

# Usuario da sesion de consola ACTIVA (mesmo criterio que win_bloqueo.ps1):
# non vale o primeiro explorer.exe se hai varias sesions abertas a vez.
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
if (-not $u) { 'desbloqueo feito (ninguen conectado)'; return }

$av = Join-Path $env:ProgramData 'piztu_desbloqueo_aviso.ps1'
$aviso = @'
$ErrorActionPreference = 'SilentlyContinue'
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
"@
try { [Snd]::Mute($false) } catch {}
(New-Object -ComObject WScript.Shell).Popup('EQUIPO DESBLOQUEADO.  Podes continuar co teu traballo.', 3, 'Control de aula', 64) | Out-Null
'@
Set-Content -Path $av -Value $aviso -Encoding UTF8

$tr = 'powershell -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' + $av
schtasks /create /tn PiztuDesbloqueo /f /sc ONCE /st 23:59 /it /ru $u /tr $tr | Out-Null
schtasks /run /tn PiztuDesbloqueo | Out-Null
"desbloqueo feito para $u"
