#!/bin/bash
# Lanza piztu sen terminal visible, redirixindo toda a saída a un log
cd /opt/piztu
exec /opt/piztu/piztu \
    >> /opt/piztu/piztu.log 2>&1
