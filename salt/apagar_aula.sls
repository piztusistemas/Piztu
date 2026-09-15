# salt/apagar_aula.sls
# Espello de playbooks/apagar_aula.yaml — apaga o equipo.
# Aplícase con:  salt <host> state.apply apagar_aula

apagar_equipo:
  cmd.run:
    - name: /sbin/shutdown -h now
