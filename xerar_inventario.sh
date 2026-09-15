#!/bin/bash

# Script para xerar inventario con MACs
# Executa o playbook Ansible e mostra a saída nun ficheiro de log
# VERSIÓN MEJORADA CON UNBUFFERED OUTPUT

HOSTS_FILE="/opt/piztu/hosts"
PLAYBOOK="/opt/piztu/playbooks/xerarInventario.yaml"
LOG_FILE="/tmp/xerar_inventario.log"

# Limpiar log anterior
> "$LOG_FILE"

# Función para escribir logs
log_msg() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - $1" | tee -a "$LOG_FILE"
}

# Función para escribir erro
log_err() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') - ❌ $1" | tee -a "$LOG_FILE" >&2
}

log_msg "🔄 Iniciando xeración do inventario con MACs..."
log_msg ""

# Verificar que o ficheiro hosts existe
if [ ! -f "$HOSTS_FILE" ]; then
    log_err "Ficheiro $HOSTS_FILE non atopado"
    exit 1
fi

# Verificar que o playbook existe
if [ ! -f "$PLAYBOOK" ]; then
    log_err "Playbook $PLAYBOOK non atopado"
    exit 1
fi

log_msg "📋 Ficheiro inventario: $HOSTS_FILE"
log_msg "📄 Playbook: $PLAYBOOK"
log_msg "📝 Log: $LOG_FILE"
log_msg ""

# Executar o playbook e escribir toda a saída no log
log_msg "⏳ Executando playbook..."
log_msg "Comando: ansible-playbook -i $HOSTS_FILE $PLAYBOOK -K"
log_msg ""

PYTHONUNBUFFERED=1 ansible-playbook -i "$HOSTS_FILE" "$PLAYBOOK" -K 2>&1 | tee -a "$LOG_FILE"
RC=${PIPESTATUS[0]}

log_msg ""

# Verificar o resultado
if [ $RC -eq 0 ]; then
    log_msg "✅ INVENTARIO XERADO CORRECTAMENTE"
    log_msg ""
    log_msg "📝 Contido final de $HOSTS_FILE:"
    cat "$HOSTS_FILE" | tee -a "$LOG_FILE"
    log_msg ""
    log_msg "✨ Proceso completado satisfactoriamente"
    exit 0
else
    log_err "Erro ao executar o playbook (código: $RC)"
    exit 1
fi
