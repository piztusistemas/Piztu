# win_aviso_ruido.ps1 — execútao o minion (como SYSTEM) vía `cmd.script` desde
# avisoRuido.sls. Substitúe a `msg *`, que NON existe en Windows 11 Home.
# O minion está na sesión 0 e non pode debuxar na pantalla do alumno: igual que
# win_desbloqueo.ps1, programa unha tarefa ("PiztuAvisoRuido") que corre NA
# sesión do usuario conectado e amosa un aviso de ~6 s.
$ProgressPreference = 'SilentlyContinue'

# Usuario da sesión de consola ACTIVA (mesmo criterio que win_bloqueo.ps1).
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
if (-not $u) { 'aviso de ruido: ninguen conectado'; return }

$av = Join-Path $env:ProgramData 'piztu_aviso_ruido.ps1'
$aviso = @'
$ErrorActionPreference = 'SilentlyContinue'
(New-Object -ComObject WScript.Shell).Popup('DEMASIADO RUIDO NA AULA. Baixade o volume ou os equipos bloquearanse automaticamente.', 6, 'Control de aula', 48) | Out-Null
'@
Set-Content -Path $av -Value $aviso -Encoding UTF8

$tr = 'powershell -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' + $av
schtasks /create /tn PiztuAvisoRuido /f /sc ONCE /st 23:59 /it /ru $u /tr $tr | Out-Null
schtasks /run /tn PiztuAvisoRuido | Out-Null
"aviso de ruido amosado a $u"
