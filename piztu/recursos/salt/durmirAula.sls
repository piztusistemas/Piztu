# salt/durmirAula.sls
# Espello de playbooks/durmirAula.yaml — suspende o equipo.
# Aplícase con:  salt <host> state.apply durmirAula
#
# Rama por SO (ver apagar_aula.sls). En Windows non hai un "suspend" limpo por
# liña de comandos: SetSuspendState hiberna se a hibernación está activada, e
# só suspende se está desactivada — por iso desactívase antes (powercfg /h off)
# para forzar suspensión. Se se prefire hibernar, quita esa liña.

suspender_equipo:
  cmd.run:
{% if grains.get('os_family') == 'Windows' or 'cmd.exe' in grains.get('shell', '')|lower %}
    - name: 'powercfg /h off & rundll32.exe powrprof.dll,SetSuspendState 0,1,0'
{% else %}
    - name: systemctl suspend
{% endif %}
