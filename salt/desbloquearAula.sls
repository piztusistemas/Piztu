# salt/desbloquearAula.sls
# Espello de playbooks/desbloquearAula.yaml — desbloqueo do equipo.
# Aplícase con:  salt <host> state.apply desbloquearAula

{% set usuario = pillar.get('usuario_alumno', 'usuario') %}

activar_rato_e_teclado:
  cmd.run:
    - name: |
        export DISPLAY=:0
        USER_ID=$(id -u {{ usuario }})

        # Mesmo ficheiro de autoridade real que bloqueoTotal.sls (non
        # ~/.Xauthority — obsoleto nesta imaxe clonada; ver comentario alí).
        XAUTH_REAL=$(ps -eo args= -C Xorg | grep -oP '(?<=-auth )\S+' | head -n1)
        XAUTH_REAL=${XAUTH_REAL:-/home/{{ usuario }}/.Xauthority}

        if command -v xinput > /dev/null; then
          for id in $(sudo -u {{ usuario }} DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput list | grep -v XTEST | grep -oP 'id=\K[0-9]+'); do
            case "$id" in ''|*[!0-9]*) continue ;; esac
            if [ "$id" -gt 4 ]; then
              sudo -u {{ usuario }} DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput enable "$id" || true
            fi
          done
        fi
    - shell: /bin/bash

quitar_aviso_e_mostrar_exito:
  cmd.run:
    - name: |
        export DISPLAY=:0
        export XAUTHORITY=/home/{{ usuario }}/.Xauthority
        USER_ID=$(id -u {{ usuario }})
        export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus

        # O kill polo PID gardado pode fallar (o proceso real de zenity vive
        # baixo un "sudo -u", e o sinal non sempre chega a través dese salto
        # de usuario) e deixar o bucle orfo executándose para sempre aínda
        # que o centinela xa non exista — por iso tamén se mata por nome,
        # sen depender do PID gardado.
        if [ -f /tmp/piztu_bloqueo.pid ]; then
          kill "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null || true
          rm -f /tmp/piztu_bloqueo.pid
        fi
        pkill -u {{ usuario }} -x zenity 2>/dev/null || true

        # LC_ALL=C.UTF-8: ver comentario en bloqueoTotal.sls
        sudo -u {{ usuario }} LC_ALL=C.UTF-8 DISPLAY=:0 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus \
        zenity --info --title="CONTROL DE AULA" \
        --text="<span size='xx-large' weight='bold'>EQUIPO DESBLOQUEADO</span>\n\nPodes continuar co teu traballo." \
        --timeout=5 --no-wrap &
    - shell: /bin/bash

restaurar_son:
  cmd.run:
    - name: amixer set Master unmute

# Sempre se executa, independentemente do que estivese bloqueado (báleiran
# cadeas baleiras ou inexistentes é inócuo). Localízase por ruta absoluta
# (non por PATH — ver bloqueoTotal.sls) e báleiran nft, iptables e ip6tables
# por igual (a meirande parte dos equipos son nftables puro, pero non se sabe
# de antemán cal usou cada un).
restaurar_rede:
  cmd.run:
    - name: |
        for p in /usr/sbin/nft /sbin/nft /usr/bin/nft /bin/nft; do
          if [ -x "$p" ]; then "$p" flush chain inet piztu_bloqueo output 2>/dev/null || true; break; fi
        done
        for bin in iptables ip6tables; do
          for p in "/usr/sbin/$bin" "/sbin/$bin" "/usr/bin/$bin" "/bin/$bin"; do
            if [ -x "$p" ]; then "$p" -F PIZTU_BLOQUEO 2>/dev/null || true; break; fi
          done
        done
    - shell: /bin/bash
