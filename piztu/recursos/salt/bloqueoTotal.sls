# salt/bloqueoTotal.sls
# Espello de playbooks/bloqueoTotal.yaml — bloqueo físico do equipo.
# Traducido a cmd.run (o playbook orixinal era shell puro).
#
# O usuario alumno pode axustarse vía pillar; por defecto "usuario".
# As opcións configurables (← pantalla de configuración de "Bloquear" en
# piztu) pásanse tamén vía pillar dende ExecutarBloqueo.
# Aplícase con:  salt <host> state.apply bloqueoTotal pillar='{"bloquear_son": true, ...}'

{% set usuario = pillar.get('usuario_alumno', 'usuario') %}
{% set bloquear_internet = pillar.get('bloquear_internet', false) %}
{% set bloquear_ssh = pillar.get('bloquear_ssh', false) %}
{% set bloquear_son = pillar.get('bloquear_son', false) %}
{% set bloquear_rato = pillar.get('bloquear_rato', true) %}
{% set bloquear_teclado = pillar.get('bloquear_teclado', true) %}

bloquear_entrada_e_avisar:
  cmd.run:
    - name: |
        export DISPLAY=:0
        export XAUTHORITY=/home/{{ usuario }}/.Xauthority
        USER_ID=$(id -u {{ usuario }})
        export DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus

        # O ficheiro de autoridade real NON é ~/.Xauthority (nesta imaxe está
        # obsoleto/mesturado con cookies doutros nomes de equipo anteriores:
        # abalar12, tecno... — imaxe clonada reutilizada). O cookie que Xorg
        # usa de verdade está no seu propio argumento -auth; sen el, xinput
        # conecta pero queda "non confiable" e as escritas (disable/enable)
        # fallan con X BadAccess aínda executándoas coma o usuario da sesión.
        #
        # Selecciónase o proceso por executable (ps -C Xorg) e non filtrando
        # "ps -eo cmd" cun grep: ese grep casaba tamén co propio proceso deste
        # script (o comentario de aquí arriba menciona "Xorg"), e esa liña
        # contén "-auth ", así que XAUTH_REAL quedaba con varias liñas de lixo
        # e xinput fallaba con BadAccess — o mesmo erro que isto quere evitar.
        XAUTH_REAL=$(ps -eo args= -C Xorg | grep -oP '(?<=-auth )\S+' | head -n1)
        XAUTH_REAL=${XAUTH_REAL:-/home/{{ usuario }}/.Xauthority}

        # 1. Bloquear rato e/ou teclado (independentes: distínguense por tipo
        # de dispositivo na propia saída de "xinput list", non só por ID).
        if command -v xinput > /dev/null; then
          LISTAXINPUT=$(sudo -u {{ usuario }} DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput list)

        {% if bloquear_rato %}
          echo "$LISTAXINPUT" | grep -i pointer | grep -v XTEST | grep -oP 'id=\K[0-9]+' | while read -r id; do
            if [ "$id" -gt 4 ]; then
              sudo -u {{ usuario }} DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput disable "$id" || true
            fi
          done
        {% endif %}
        {% if bloquear_teclado %}
          echo "$LISTAXINPUT" | grep -i keyboard | grep -v XTEST | grep -oP 'id=\K[0-9]+' | while read -r id; do
            if [ "$id" -gt 4 ]; then
              sudo -u {{ usuario }} DISPLAY=:0 XAUTHORITY="$XAUTH_REAL" xinput disable "$id" || true
            fi
          done
        {% endif %}
        fi

        {% if bloquear_rato or bloquear_teclado %}
        # 2. Aviso persistente en segundo plano. Só se rato e/ou teclado
        # están bloqueados (non ten sentido se o alumno pode seguir
        # escribindo/movendo o rato con normalidade — internet/SSH/son non a
        # amosan). Remata só cando o desbloqueo mata o PID gardado aquí.
        # Antes de lanzar un bucle novo, mátase calquera bucle anterior (por
        # PID e por nome) — se un bloqueo previo non se desbloqueou ben,
        # evita amorear varios avisos zenity executándose á vez para sempre.
        if [ -f /tmp/piztu_bloqueo.pid ]; then
          kill "$(cat /tmp/piztu_bloqueo.pid)" 2>/dev/null || true
        fi
        pkill -u {{ usuario }} -x zenity 2>/dev/null || true

        (
          while true; do
            # LC_ALL=C.UTF-8: sudo -u non conserva LC_CTYPE (queda en "C",
            # só ASCII) aínda que LANG si se manteña — sen isto zenity
            # rexeita o texto en galego con tiles ("This option is not
            # available", sae ao instante e nunca se ve o aviso).
            sudo -u {{ usuario }} LC_ALL=C.UTF-8 DISPLAY=:0 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_ID/bus \
              zenity --warning --title="CONTROL DE AULA" \
              --text="<span size='xx-large' weight='bold'>EQUIPO BLOQUEADO</span>\n\nPresta atención ás indicacións do profesor." \
              --no-wrap
            sleep 1
          done
        ) < /dev/null > /dev/null 2>&1 &
        echo $! > /tmp/piztu_bloqueo.pid
        {% endif %}
    - shell: /bin/bash

{% if bloquear_son %}
mutear_son:
  cmd.run:
    - name: amixer set Master mute
{% endif %}

# Bloqueo de rede: só corta o que INICIA o propio equipo (cadea OUTPUT),
# nunca o que entra — a conexión coa que Piztu controla o equipo (o propio
# minion de Salt cara ao master) é sempre saínte por outro porto (4505/4506),
# e a de SSH (motor Ansible) é sempre entrante, así que non se ven afectadas.
# A cadea PIZTU_BLOQUEO é propia e idempotente (báleirase antes de
# reconstruírse), para non tocar outras regras de iptables que xa existisen.
#
# Localízase por ruta absoluta (non por PATH: baixo unha shell non
# interactiva "command -v" pode non atopar /usr/sbin/iptables aínda que
# exista).
#
# CAUSA REAL de que isto non funcionase (verificado nunha máquina real,
# bac01): as imaxes actuais NON teñen iptables/ip6tables instalados en
# absoluto — son nftables puro (só existe "nft"). Úsase nft coma primeira
# opción (unha soa táboa "inet" cobre IPv4+IPv6 á vez), con
# iptables/ip6tables coma reserva para equipos máis antigos.
{% if bloquear_internet or bloquear_ssh %}
bloquear_rede:
  cmd.run:
    - name: |
        localizar() {
          for p in "/usr/sbin/$1" "/sbin/$1" "/usr/bin/$1" "/bin/$1"; do
            if [ -x "$p" ]; then echo "$p"; return 0; fi
          done
          return 1
        }

        NFT=$(localizar nft) || NFT=""
        echo "nft: ${NFT:-non atopado}"

        if [ -n "$NFT" ]; then
          "$NFT" add table inet piztu_bloqueo 2>/dev/null
          "$NFT" 'add chain inet piztu_bloqueo output { type filter hook output priority 0 ; }' 2>/dev/null
          "$NFT" flush chain inet piztu_bloqueo output

          "$NFT" add rule inet piztu_bloqueo output oifname "lo" accept
          "$NFT" add rule inet piztu_bloqueo output ct state established,related accept
        {% if bloquear_ssh %}
          "$NFT" add rule inet piztu_bloqueo output tcp dport 22 drop
        {% endif %}
        {% if bloquear_internet %}
          "$NFT" add rule inet piztu_bloqueo output ip daddr 10.0.0.0/8 accept
          "$NFT" add rule inet piztu_bloqueo output ip daddr 172.16.0.0/12 accept
          "$NFT" add rule inet piztu_bloqueo output ip daddr 192.168.0.0/16 accept
          "$NFT" add rule inet piztu_bloqueo output ip6 daddr fe80::/10 accept
          "$NFT" add rule inet piztu_bloqueo output ip6 daddr fc00::/7 accept
          "$NFT" add rule inet piztu_bloqueo output drop
        {% endif %}
          echo "Regras nftables (IPv4+IPv6) aplicadas."
          "$NFT" list chain inet piztu_bloqueo output
          exit 0
        fi

        IPT=$(localizar iptables) || IPT=""
        IPT6=$(localizar ip6tables) || IPT6=""
        echo "iptables: ${IPT:-non atopado} | ip6tables: ${IPT6:-non atopado}"

        if [ -z "$IPT" ] && [ -z "$IPT6" ]; then
          echo "Nin nft, nin iptables, nin ip6tables atopados; bloqueo de rede omitido"
          exit 0
        fi

        if [ -n "$IPT" ]; then
          "$IPT" -N PIZTU_BLOQUEO 2>/dev/null
          "$IPT" -F PIZTU_BLOQUEO
          "$IPT" -C OUTPUT -j PIZTU_BLOQUEO 2>/dev/null || "$IPT" -I OUTPUT -j PIZTU_BLOQUEO
          "$IPT" -A PIZTU_BLOQUEO -o lo -j RETURN
          "$IPT" -A PIZTU_BLOQUEO -m state --state ESTABLISHED,RELATED -j RETURN
        {% if bloquear_ssh %}
          "$IPT" -A PIZTU_BLOQUEO -p tcp --dport 22 -j DROP
        {% endif %}
        {% if bloquear_internet %}
          "$IPT" -A PIZTU_BLOQUEO -d 10.0.0.0/8 -j RETURN
          "$IPT" -A PIZTU_BLOQUEO -d 172.16.0.0/12 -j RETURN
          "$IPT" -A PIZTU_BLOQUEO -d 192.168.0.0/16 -j RETURN
          "$IPT" -A PIZTU_BLOQUEO -j DROP
        {% endif %}
          echo "Regras IPv4 aplicadas."
        fi

        if [ -n "$IPT6" ]; then
          "$IPT6" -N PIZTU_BLOQUEO 2>/dev/null
          "$IPT6" -F PIZTU_BLOQUEO
          "$IPT6" -C OUTPUT -j PIZTU_BLOQUEO 2>/dev/null || "$IPT6" -I OUTPUT -j PIZTU_BLOQUEO
          "$IPT6" -A PIZTU_BLOQUEO -o lo -j RETURN
          "$IPT6" -A PIZTU_BLOQUEO -m state --state ESTABLISHED,RELATED -j RETURN
        {% if bloquear_ssh %}
          "$IPT6" -A PIZTU_BLOQUEO -p tcp --dport 22 -j DROP
        {% endif %}
        {% if bloquear_internet %}
          # Equivalente IPv6 do "permitir rede local": link-local (fe80::/10,
          # imprescindible para ARP/veciñanza e resolución mDNS de nomes
          # .local) e unique-local (fc00::/7). Sen isto, calquera control que
          # resolva por mDNS/avahi (habitual: os nomes .local adoitan
          # resolver a un enderezo link-local) queda tan bloqueado coma
          # internet real.
          "$IPT6" -A PIZTU_BLOQUEO -d fe80::/10 -j RETURN
          "$IPT6" -A PIZTU_BLOQUEO -d fc00::/7 -j RETURN
          "$IPT6" -A PIZTU_BLOQUEO -j DROP
        {% endif %}
          echo "Regras IPv6 aplicadas."
        fi
    - shell: /bin/bash
{% endif %}
