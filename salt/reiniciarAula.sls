# salt/reiniciarAula.sls
# Espello de playbooks/reiniciarAula.yaml — reinicia o equipo.
# Aplícase con:  salt <host> state.apply reiniciarAula

reiniciar_equipo:
  cmd.run:
    - name: /sbin/shutdown -r now
