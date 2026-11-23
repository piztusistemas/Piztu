# Configurar un equipo Windows do alumnado (manual)

Desde a versión 26.12.2, Piztu non xera ningún preparador para Windows: cada
equipo configúrase a man, unha vez, seguindo estes pasos. Valen para
Windows 10 e 11 (Home, Pro ou Education).

## Modelo: a conta da aula NON é administradora

- O alumnado traballa cunha **conta estándar** (limitada). É a conta coa que
  Piztu se conecta por SSH (⚙ Aula → *Usuario dos equipos*, por defecto
  `usuario`). Por ela van só as prácticas e a copia de Tao, que caen no perfil
  do propio usuario.
- Todo o que precisa permisos (apagar, durmir, bloquear, firewall, instalar
  Tao en `C:\Program Files`, scripts de Xesta) faino o **minion de Salt**, que
  corre como SYSTEM. Por iso, nas aulas con equipos Windows o motor ten que
  ser **Salt**.
- Os motores SSH e Ansible executan as accións de Windows por SSH coa conta da
  aula, e iso só funciona se é administradora. Cunha conta estándar Piztu
  avisa de que hai que usar o motor Salt.

Para facer a configuración necesitas unha conta de **administrador** distinta
da da aula, por exemplo a que se creou ao instalar Windows. Abre unha
**PowerShell como administrador** con esa conta. No resto da guía
`usuario` é o nome da conta da aula: cámbiao se en Piztu tes outro.

```powershell
$u = 'usuario'      # conta da aula (⚙ Aula → Usuario dos equipos)
```

## 1. OpenSSH Server

```powershell
Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0
Set-Service -Name sshd -StartupType Automatic
Start-Service sshd
# Shell por defecto: cmd (a de serie). Se alguén a cambiou, volver a cmd:
Remove-ItemProperty -Path HKLM:\SOFTWARE\OpenSSH -Name DefaultShell -ErrorAction SilentlyContinue
```

## 2. Conta da aula, estándar

Se a conta non existe, créaa e métea no grupo Usuarios. Os grupos van polo
SID, porque o nome cambia segundo o idioma de Windows.

```powershell
$p = Read-Host "Contrasinal para $u" -AsSecureString
New-LocalUser -Name $u -Password $p -PasswordNeverExpires -AccountNeverExpires
Add-LocalGroupMember -SID S-1-5-32-545 -Member $u          # Usuarios
```

Asegúrate de que **non** está en Administradores. Se xa existía e era
administradora, isto quítalla:

```powershell
Remove-LocalGroupMember -SID S-1-5-32-544 -Member $u -ErrorAction SilentlyContinue
```

Despois **inicia sesión unha vez con esa conta** e pecha a sesión. Así Windows
crea o perfil `C:\Users\usuario`, que o paso seguinte necesita.

## 3. Clave SSH da aula

No servidor Piztu, colle a clave pública da aula. Se aínda non existe, créaa
con ⚙ Aula → *Crear e enviar clave SSH*, ou a man:

```bash
ls ~/.ssh/id_rsa_piztu_aula.pub || ssh-keygen -t rsa -b 4096 -N "" -f ~/.ssh/id_rsa_piztu_aula
cat ~/.ssh/id_rsa_piztu_aula.pub
```

En Windows, coa PowerShell de administrador, pega a liña completa
(`ssh-rsa AAAA… usuario@servidor`) entre as comiñas:

```powershell
$clave = 'ssh-rsa AAAA... usuario@servidor'
$d = "C:\Users\$u\.ssh"
New-Item -ItemType Directory -Force -Path $d | Out-Null
Set-Content -Encoding ascii -Path "$d\authorized_keys" -Value $clave
icacls "$d\authorized_keys" /inheritance:r /grant "${u}:F" /grant *S-1-5-18:F /grant *S-1-5-32-544:F
```

- A clave vai no perfil do usuario porque a conta é **estándar**. O ficheiro
  `C:\ProgramData\ssh\administrators_authorized_keys` só o le `sshd` para as
  contas de administrador.
- `-Encoding ascii` é obrigatorio: Windows PowerShell 5.1 gardaría o ficheiro
  en UTF-16, e `sshd` non o le.
- O `icacls` deixa o ficheiro só para o usuario, SYSTEM e Administradores. Se
  ten permisos máis abertos, `sshd` ignórao sen avisar.

**Se o equipo estaba preparado cunha versión anterior** (conta administradora),
quita a clave da aula do ficheiro de administradores:

```powershell
$ak = 'C:\ProgramData\ssh\administrators_authorized_keys'
if (Test-Path $ak) { (Get-Content $ak) | Where-Object { $_.Trim() -ne $clave.Trim() } | Set-Content -Encoding ascii $ak }
```

## 4. Salt minion

O `id` do minion ten que ser **o nome do equipo tal como está no inventario
de Piztu**, normalmente o nome en minúsculas máis o dominio do servidor
(`bac01.local`). `MASTER` é o enderezo do servidor Piztu.

```powershell
$master = 'bac0.local'
$id     = $env:COMPUTERNAME.ToLower() + '.local'
$ver    = '3008.2'
$msi    = "Salt-Minion-$ver-Py3-AMD64.msi"
Invoke-WebRequest -UseBasicParsing -OutFile "$env:TEMP\$msi" `
    -Uri "https://packages.broadcom.com/artifactory/saltproject-generic/windows/$ver/$msi"
Start-Process msiexec.exe -Wait -ArgumentList "/i `"$env:TEMP\$msi`" /qn /norestart MASTER=$master MINION_ID=$id START_MINION=1"
Set-Service salt-minion -StartupType Automatic
```

Se o minion xa estaba instalado e só hai que cambiarlle o master ou o id:

```powershell
$conf = 'C:\ProgramData\Salt Project\Salt\conf\minion.d'
New-Item -ItemType Directory -Force -Path $conf | Out-Null
Set-Content -Encoding ascii -Path "$conf\piztu.conf" -Value "master: $master", "id: $id"
Restart-Service salt-minion
```

No servidor, se Piztu non acepta a clave do minion automaticamente:

```bash
sudo salt-key -L            # o equipo aparece en "Unaccepted Keys"
sudo salt-key -a bac01.local
```

## 5. Firewall

Piztu usa ping para saber se o equipo está acendido, e Windows bloquéao de
serie.

```powershell
netsh advfirewall firewall add rule name=Piztu-Ping protocol=icmpv4:8,any dir=in action=allow
netsh advfirewall firewall add rule name=Piztu-SSH protocol=TCP localport=22 dir=in action=allow
```

## 6. Wake-on-LAN

```powershell
# Sen "inicio rápido": con el, un equipo apagado está hibernado e moitas tarxetas non espertan
Set-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Power' -Name HiberbootEnabled -Value 0 -Type DWord
# WoL na tarxeta de rede (se falla, actívao na BIOS/UEFI)
Get-NetAdapter -Physical | Where-Object Status -eq 'Up' | ForEach-Object { Set-NetAdapterPowerManagement -Name $_.Name -WakeOnMagicPacket Enabled }
# Ao espertar dunha suspensión, entrar directo á sesión sen pedir contrasinal
powercfg /setacvalueindex SCHEME_CURRENT SUB_NONE CONSOLELOCK 0
powercfg /setdcvalueindex SCHEME_CURRENT SUB_NONE CONSOLELOCK 0
powercfg /setactive SCHEME_CURRENT
```

## 7. Inventario de Piztu

Engade o equipo ao inventario (`hosts`) co mesmo nome que o `id` do minion. A
MAC sácaa ⚙ Aula → *Escanear rede*, ou podes poñela a man:

```powershell
(Get-NetAdapter -Physical | Where-Object Status -eq 'Up' | Select-Object -First 1).MacAddress -replace '-', ':'
```

```
bac01.local mac=aa:bb:cc:dd:ee:ff so=windows
```

`so=windows` é opcional, porque Piztu detecta o SO, pero así non ten que
preguntalo.

## 8. Comprobar desde o servidor

```bash
sudo salt bac01.local test.ping                                   # True
ssh -i ~/.ssh/id_rsa_piztu_aula usuario@bac01.local hostname       # sen pedir contrasinal
ssh -i ~/.ssh/id_rsa_piztu_aula usuario@bac01.local "whoami /groups | findstr S-1-5-32-544"
```

O último comando **non** debe mostrar nada: se aparece `Administradores`, a
conta da aula aínda é administradora.

Por último, en Piztu escolle o motor **Salt** e proba *Bloquear* e
*Desbloquear* nese equipo.
