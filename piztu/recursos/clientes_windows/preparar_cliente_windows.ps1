# preparar_cliente_windows.ps1 — deixa un equipo Windows 10/11 do alumnado
# listo para que o administre Piztu (aula Windows ou mixta).
#
# Executar UNHA VEZ en cada equipo, coma administrador (o .bat do mesmo
# cartafol pídeo só). Piztu xera unha copia xa enchida co enderezo do servidor
# e a clave da aula en <base_dir>/clientes_windows/ — usa esa e non precisas
# pasar ningún parámetro.
#
# Que fai (idempotente: pódese repetir sen romper nada):
#   1. OpenSSH Server: instala, arrinca e deixa automático (motor SSH, envío
#      e recollida de prácticas, instalar Tao).
#   2. Conta da aula (-Usuario): créaa se falta e métea en Administradores
#      — o equivalente Windows ao `sudo NOPASSWD` que Piztu pide en Linux.
#   3. Autoriza a clave pública da aula (con ACL correcta: OpenSSH ignora o
#      ficheiro en silencio se ten permisos laxos).
#   4. Salt minion apuntando ao master de Piztu. O seu id TEN que ser o nome
#      do equipo tal como está no inventario de Piztu (hosts), que é a quen
#      apunta `salt <equipo>`: por defecto, nome do equipo + o dominio do
#      servidor (servidor bac0.local → bac07.local). -IdMinion para outro.
#   5. Firewall: permite ping (o mapa de Piztu usa ping para saber se o
#      equipo está acendido; Windows bloquéao por defecto) e SSH.
#   6. Wake-on-LAN: desactiva o "inicio rápido" (con el, un equipo "apagado"
#      está hibernado e moitas tarxetas non espertan), activa o WoL da tarxeta
#      e quita o "pedir inicio de sesión ao espertar" da suspensión.
#
# Uso manual:
#   powershell -ExecutionPolicy Bypass -File preparar_cliente_windows.ps1 `
#       -Servidor bac0.local -ClavePublica 'ssh-rsa AAAA… usuario@bac0'
#   (-Contrasinal para crear a conta se non existe)

[CmdletBinding()]
param(
    [string]$Servidor     = '@@SERVIDOR@@',
    [string]$ClavePublica = '@@CLAVE_PUBLICA@@',
    [string]$Usuario      = '@@USUARIO@@',
    [string]$Contrasinal  = '',
    [string]$VersionSalt  = '3008.2',
    [string]$IdMinion     = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference    = 'SilentlyContinue'

function Paso($t)  { Write-Host "`n==> $t" -ForegroundColor Cyan }
function Ben($t)   { Write-Host "    OK  $t" -ForegroundColor Green }
function Aviso($t) { Write-Host "    !!  $t" -ForegroundColor Yellow }

# Marcadores sen substituír (script orixinal, non a copia xerada por Piztu).
if ($Servidor -like '@@*')     { $Servidor = '' }
if ($ClavePublica -like '@@*') { $ClavePublica = '' }
if ($Usuario -like '@@*' -or -not $Usuario) { $Usuario = 'usuario' }

# ── 0. Administrador? ─────────────────────────────────────────────────────────
$eu = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $eu.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Host 'Este script ten que executarse coma administrador (clic dereito → Executar como administrador).' -ForegroundColor Red
    exit 1
}
if (-not $ClavePublica) {
    Write-Host 'Falta a clave pública da aula (-ClavePublica). Usa a copia que xera Piztu en <base_dir>\clientes_windows\.' -ForegroundColor Red
    exit 1
}
if (-not $Servidor) {
    Write-Host 'Falta o enderezo do servidor Piztu (-Servidor), que é o Salt master.' -ForegroundColor Red
    exit 1
}

# SID do grupo Administradores (S-1-5-32-544): o nome cambia co idioma
# ("Administradores", "Administrators"…), o SID non.
$grupoAdmin = (Get-LocalGroup -SID 'S-1-5-32-544').Name

# ── 1. OpenSSH Server ─────────────────────────────────────────────────────────
Paso 'OpenSSH Server'
$cap = Get-WindowsCapability -Online -Name 'OpenSSH.Server*' | Select-Object -First 1
if ($cap.State -ne 'Installed') {
    Add-WindowsCapability -Online -Name $cap.Name | Out-Null
    Ben 'instalado'
} else { Ben 'xa estaba instalado' }
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
# cmd.exe coma shell por defecto (a de serie): os scripts de Piztu contan con ela
# para a sonda de SO (`uname -s || ver`) e lanzan PowerShell eles mesmos.
New-Item -Path 'HKLM:\SOFTWARE\OpenSSH' -Force | Out-Null
Remove-ItemProperty -Path 'HKLM:\SOFTWARE\OpenSSH' -Name DefaultShell -ErrorAction SilentlyContinue
Ben 'servizo sshd activo e automático'

# ── 2. Conta da aula ──────────────────────────────────────────────────────────
Paso "Conta '$Usuario'"
$conta = Get-LocalUser -Name $Usuario -ErrorAction SilentlyContinue
if (-not $conta) {
    if (-not $Contrasinal) {
        $seg = Read-Host "A conta '$Usuario' non existe. Contrasinal para creala" -AsSecureString
    } else {
        $seg = ConvertTo-SecureString $Contrasinal -AsPlainText -Force
    }
    $conta = New-LocalUser -Name $Usuario -Password $seg -PasswordNeverExpires -AccountNeverExpires
    Ben 'creada'
} else { Ben 'xa existe' }
$sidConta = $conta.SID.Value
$eAdmin = [bool](Get-LocalGroupMember -SID 'S-1-5-32-544' -ErrorAction SilentlyContinue |
                 Where-Object { $_.SID.Value -eq $sidConta })
if (-not $eAdmin) {
    Add-LocalGroupMember -SID 'S-1-5-32-544' -Member $Usuario
    Ben "engadida a $grupoAdmin"
} else { Ben "xa é de $grupoAdmin" }

# ── 3. Clave pública da aula ──────────────────────────────────────────────────
Paso 'Clave SSH da aula'
# TRAMPA de OpenSSH en Windows: para membros de Administradores, o sshd_config
# por defecto IGNORA ~/.ssh/authorized_keys e só le este ficheiro, que ademais
# ten que ser só de SYSTEM e Administradores (senón tamén se ignora, sen erro).
$ak = Join-Path $env:ProgramData 'ssh\administrators_authorized_keys'
New-Item -ItemType Directory -Force -Path (Split-Path $ak) | Out-Null
$actual = @()
if (Test-Path $ak) { $actual = Get-Content $ak -ErrorAction SilentlyContinue }
$clave = $ClavePublica.Trim()
if ($actual -notcontains $clave) {
    Add-Content -Path $ak -Value $clave -Encoding ascii
    Ben 'clave engadida'
} else { Ben 'a clave xa estaba autorizada' }
icacls.exe $ak /inheritance:r /grant '*S-1-5-32-544:F' /grant '*S-1-5-18:F' | Out-Null
Ben 'permisos do ficheiro axustados'
Restart-Service sshd

# ── 4. Salt minion ────────────────────────────────────────────────────────────
Paso "Salt minion $VersionSalt → master $Servidor"
$id = $IdMinion
if (-not $id) {
    $id = $env:COMPUTERNAME.ToLower()
    $i = $Servidor.IndexOf('.')
    if ($i -gt 0 -and -not ($Servidor -match '^\d+(\.\d+){3}$')) { $id += $Servidor.Substring($i) }
}
$svc = Get-Service salt-minion -ErrorAction SilentlyContinue
if (-not $svc) {
    $msi = "Salt-Minion-$VersionSalt-Py3-AMD64.msi"
    $url = "https://packages.broadcom.com/artifactory/saltproject-generic/windows/$VersionSalt/$msi"
    $tmp = Join-Path $env:TEMP $msi
    Write-Host "    baixando $url"
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
    $p = Start-Process msiexec.exe -Wait -PassThru -ArgumentList @(
        '/i', "`"$tmp`"", '/qn', '/norestart',
        "MASTER=$Servidor", "MINION_ID=$id", 'START_MINION=1')
    if ($p.ExitCode -ne 0) { throw "msiexec devolveu $($p.ExitCode)" }
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Ben "instalado (id: $id)"
} else {
    # Xa instalado: só se reapunta ao master por se cambiou.
    $conf = Join-Path $env:ProgramData 'Salt Project\Salt\conf\minion.d'
    New-Item -ItemType Directory -Force -Path $conf | Out-Null
    Set-Content -Path (Join-Path $conf 'piztu.conf') -Encoding ascii -Value @(
        "master: $Servidor", "id: $id")
    Restart-Service salt-minion
    Ben "xa estaba instalado; reapuntado a $Servidor (id: $id)"
}
Set-Service salt-minion -StartupType Automatic

# ── 5. Firewall ───────────────────────────────────────────────────────────────
Paso 'Firewall'
netsh advfirewall firewall delete rule name=Piztu-Ping | Out-Null
netsh advfirewall firewall add rule name=Piztu-Ping protocol=icmpv4:8,any dir=in action=allow | Out-Null
netsh advfirewall firewall delete rule name=Piztu-SSH | Out-Null
netsh advfirewall firewall add rule name=Piztu-SSH protocol=TCP localport=22 dir=in action=allow | Out-Null
Ben 'ping e SSH permitidos'

# ── 6. Wake-on-LAN ────────────────────────────────────────────────────────────
Paso 'Wake-on-LAN'
Set-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Power' -Name HiberbootEnabled -Value 0 -Type DWord
Ben 'inicio rápido desactivado'
foreach ($a in Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Where-Object Status -eq 'Up') {
    try {
        Set-NetAdapterPowerManagement -Name $a.Name -WakeOnMagicPacket Enabled -ErrorAction Stop
        Ben "WoL activado en '$($a.Name)'"
    } catch { Aviso "a tarxeta '$($a.Name)' non deixa activar WoL dende aquí (revísao na BIOS)" }
}
# Ao espertar dunha suspensión (Durmir → Acender dende Piztu) entrar directo á
# sesión: o autologin só actúa no arranque, e con CONSOLELOCK=1 (o valor de
# fábrica) Windows amosa a pantalla de inicio de sesión (visto en bac01).
powercfg /setacvalueindex SCHEME_CURRENT SUB_NONE CONSOLELOCK 0
powercfg /setdcvalueindex SCHEME_CURRENT SUB_NONE CONSOLELOCK 0
powercfg /setactive SCHEME_CURRENT
Ben 'sen pedir inicio de sesión ao espertar'

# ── Resumo ────────────────────────────────────────────────────────────────────
Paso 'Feito. Datos deste equipo para o inventario de Piztu:'
$mac = (Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Where-Object Status -eq 'Up' |
        Select-Object -First 1).MacAddress -replace '-', ':'
Write-Host "    $id  mac=$($mac.ToLower())  so=windows" -ForegroundColor White
Write-Host "`n    (so=windows no ficheiro hosts é opcional: Piztu detecta o SO só, pero así non ten que preguntar.)"
