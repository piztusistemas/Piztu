# salt/durmirAula.sls
# Espello de playbooks/durmirAula.yaml — suspende o equipo.
# Aplícase con:  salt <host> state.apply durmirAula

suspender_equipo:
  cmd.run:
    - name: systemctl suspend
