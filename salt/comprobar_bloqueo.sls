# salt/comprobar_bloqueo.sls
# Espello de playbooks/comprobar_bloqueo.yaml — informa se o equipo ten
# activo un proceso de bloqueo de pantalla.
#
# Nota: o monitor de ruído (internal/ruido) verifica sempre por Ansible
# directamente, non pasa por este estado nin polo motor activo; este .sls
# existe só para poder chamalo manualmente (salt <host> state.apply
# comprobar_bloqueo) e ver o resultado no log.
#
# Aplícase con:  salt <host> state.apply comprobar_bloqueo

comprobar_procesos_bloqueo:
  cmd.run:
{% if grains.get('os_family') == 'Windows' or 'cmd.exe' in grains.get('shell', '')|lower %}
    - name: 'if exist "%ProgramData%\piztu_bloqueo.flag" (echo BLOQUEADO:aviso) else (netsh advfirewall firewall show rule name=PIZTU_BLOQUEO >nul 2>&1 && echo BLOQUEADO:rede || echo LIBRE)'
{% else %}
    - name: |
        if [ -f /tmp/piztu_bloqueo.pid ] && kill -0 "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null; then
          echo "BLOQUEADO:pid"
          exit 0
        fi
        for proc in xtrlock i3lock vlock gnome-screensaver light-locker xscreensaver dm-tool zenity; do
          if pgrep -x "$proc" > /dev/null 2>&1; then
            echo "BLOQUEADO:$proc"
            exit 0
          fi
        done
        echo "LIBRE"
{% endif %}
