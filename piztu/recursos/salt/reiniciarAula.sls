# salt/reiniciarAula.sls
# Espello de playbooks/reiniciarAula.yaml — reinicia o equipo.
# Aplícase con:  salt <host> state.apply reiniciarAula
#
# Rama por SO (ver apagar_aula.sls).

reiniciar_equipo:
  cmd.run:
{% if grains.get('os_family') == 'Windows' or 'cmd.exe' in grains.get('shell', '')|lower %}
    - name: 'shutdown /r /t 0 /f'
{% else %}
    - name: '/sbin/shutdown -r now'
{% endif %}
