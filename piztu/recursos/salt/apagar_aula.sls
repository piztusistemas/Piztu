# salt/apagar_aula.sls
# Espello de playbooks/apagar_aula.yaml — apaga o equipo.
# Aplícase con:  salt <host> state.apply apagar_aula
#
# Adaptación por SO: Salt renderiza en cada minion a súa propia rama segundo
# grains['kernel'] ('Linux' | 'Windows' | 'Darwin'). piztu non precisa saber o
# SO para isto; a detección no controlador (internal/motor/so.go) só serve para
# avisar na interface e para operacións sen equivalente (envío de prácticas).

apagar_equipo:
  cmd.run:
{% if grains.get('os_family') == 'Windows' or 'cmd.exe' in grains.get('shell', '')|lower %}
    - name: 'shutdown /s /t 0 /f'
{% else %}
    - name: '/sbin/shutdown -h now'
{% endif %}
