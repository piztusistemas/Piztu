# Estados Salt en Windows

Cada `*.sls` deste cartafol leva unha rama por SO:

```jinja
{% if grains.get('os_family') == 'Windows' or 'cmd.exe' in grains.get('shell', '')|lower %}
```

Úsase o grain `shell` (non `kernel`/`os_family`) como reserva porque nalgúns
minions Windows desta aula o subsistema de grains que depende de WMI falla e
devólveos baleiros (Salt 3008 onedir co paquete danado). O mesmo dano fai que
**`file.managed` non cargue** nese minion, así que:

- os comandos curtos van por **`cmd.run`** (`netsh`, `shutdown`, `powercfg`…);
- o que precisa un script grande vai por **`cmd.script` + `salt://…ps1`**
  (usa o módulo `cp`, que si funciona, e non ten o límite de 8 KB da liña de
  comandos de `cmd.exe` que ten `powershell -EncodedCommand`).

## Cobertura por acción

| Acción | Windows |
|---|---|
| `apagar_aula` | `shutdown /s /t 0 /f` |
| `durmirAula` | `powercfg /h off & rundll32 …SetSuspendState` |
| `avisoRuido` | `win_aviso_ruido.ps1` (popup na sesión do alumno) |
| `reiniciarAula` | `shutdown /r /t 0 /f` |
| `comprobar_bloqueo` | mira a bandeira ou a regra de firewall |
| `bloqueoTotal` | firewall (internet/SSH) **+** `win_bloqueo.ps1` |
| `desbloquearAula` | `win_desbloqueo.ps1` |

## Bloqueo do equipo (`win_bloqueo.ps1` / `win_desbloqueo.ps1`)

O minion corre como **SYSTEM na sesión 0** e non pode debuxar nin bloquear a
entrada na sesión do alumno. Solución universal (Win7→11, calquera edición):

1. `bloqueoTotal.sls` → `cmd.script salt://win_bloqueo.ps1`. Ese script (SYSTEM):
   - detecta o usuario conectado (`Get-Process explorer -IncludeUserName`, sen WMI),
   - crea a bandeira `%ProgramData%\piztu_bloqueo.flag`,
   - escribe `%ProgramData%\piztu_bloqueo_overlay.ps1`,
   - `schtasks /create … /it /ru <usuario>` + `/run` da tarefa **PiztuBloqueo**.
2. **PiztuBloqueo** corre `piztu_bloqueo_overlay.ps1` NA sesión do alumno:
   - `[Snd]::Mute($true)` (Core Audio, `SetMute` no endpoint por defecto),
   - `BlockInput($true)` (user32.dll — bloquea rato+teclado; Ctrl+Alt+Del non),
   - `Form` sen bordos, maximizada, `TopMost`, sen cursor, texto "EQUIPO BLOQUEADO",
   - un `Timer` de 1 s: cando desaparece a bandeira → `Mute($false)`,
     `BlockInput($false)`, pecha. `FormClosing` cancélase mentres exista a bandeira.
3. `desbloquearAula.sls` → `cmd.script salt://win_desbloqueo.ps1` (SYSTEM):
   - borra a bandeira (o overlay libera todo e péchase só),
   - `schtasks /end`+`/delete` de PiztuBloqueo, borra as regras de firewall,
   - programa **PiztuDesbloqueo** → un aviso de ~3 s "EQUIPO DESBLOQUEADO" na
     sesión do alumno (e `Mute($false)` por se o overlay morrera sen limpar).

### Limitacións

- `BlockInput` non bloquea **Ctrl+Alt+Del** (imposible sen un helper con SAS).
- O overlay pódese minimizar con Win+D / Win+M nun intre antes de que o
  `Deactivate` o volva subir; combinado co `BlockInput` é pouco probable.
- Se a tarefa PiztuBloqueo se mata sen executar a limpeza, o `BlockInput`
  libérao o SO ao morrer o proceso, pero o **mute** só o restaura
  `win_desbloqueo.ps1` (por iso o repite).
- `msg.exe` NON existe en Windows 11 Home; por iso o aviso é unha `Form` /
  `WScript.Shell.Popup`, non `msg`.

## Preparar un equipo Windows

Unha vez por equipo: ⚙ Aula → Clave SSH → «🪟 Equipos Windows: xerar
preparador», e no PC Windows dobre clic en `preparar_cliente_windows.bat`
(fonte: `clientes_windows/`). Instala OpenSSH Server, fai administradora a
conta da aula, autoriza a clave, instala o Salt minion (id = nome do
inventario), abre o ping e activa Wake-on-LAN.

## Outros motores

- **SSH**: executa os mesmos `win_*.ps1` por SSH + PowerShell (`piztu/internal/motor/ssh_win.go`).
- **Ansible**: os equipos Windows desvíanse ao camiño do motor SSH (`derivarWindows`).
