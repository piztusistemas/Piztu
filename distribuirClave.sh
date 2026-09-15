#!/bin/bash
# distribuirClave.sh - Script para distribuír a clave SSH aos clientes

# Cores para saída
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# ═══════════════════════════════════════════════════════════════════════════════
# DETECCIÓN DO USUARIO E PERMISOS
# ═══════════════════════════════════════════════════════════════════════════════

# Determinar o usuario que executa o script
if [ -n "$SUDO_USER" ]; then
    # Execúdose con sudo
    REAL_USER="$SUDO_USER"
    REAL_HOME=$(eval echo ~$SUDO_USER)
    USING_SUDO=1
elif [ "$EUID" -eq 0 ]; then
    # Execúdose directamente como root SEN sudo
    echo -e "${RED}❌ Este script debe executarse como usuario normal (non como root)${NC}"
    echo -e "${YELLOW}Opciones:${NC}"
    echo -e "  1. Executa como usuario normal: /opt/piztu/distribuirClave.sh"
    echo -e "  2. Executa con sudo: sudo /opt/piztu/distribuirClave.sh"
    exit 1
else
    # Execúdose como usuario normal (recomendado)
    REAL_USER="$USER"
    REAL_HOME="$HOME"
    USING_SUDO=0
fi

# Directorio base de Piztu
PIZTU_DIR="/opt/piztu"
PLAYBOOK="$PIZTU_DIR/playbooks/distribuirClave.yaml"
INVENTORY="$PIZTU_DIR/hosts"
SSH_KEY_PRIV="$REAL_HOME/.ssh/id_rsa"
SSH_KEY_PUB="$REAL_HOME/.ssh/id_rsa.pub"

# ═══════════════════════════════════════════════════════════════════════════════

echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Distribuír Clave SSH aos Clientes${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════════${NC}\n"

echo -e "${BLUE}Información do usuario:${NC}"
echo -e "  Usuario: ${YELLOW}$REAL_USER${NC}"
echo -e "  Home: ${YELLOW}$REAL_HOME${NC}"
echo -e "  Claves: ${YELLOW}$SSH_KEY_PRIV${NC}"
if [ $USING_SUDO -eq 1 ]; then
    echo -e "  Modo: ${YELLOW}sudo${NC}"
else
    echo -e "  Modo: ${YELLOW}usuario normal${NC}"
fi
echo ""

# Verificar que existe o playbook
if [ ! -f "$PLAYBOOK" ]; then
    echo -e "${RED}❌ Non se atopa o playbook: $PLAYBOOK${NC}"
    exit 1
fi

# Verificar que existe o ficheiro de inventario
if [ ! -f "$INVENTORY" ]; then
    echo -e "${RED}❌ Non se atopa o ficheiro de inventario: $INVENTORY${NC}"
    exit 1
fi

# Verificar que existen as claves SSH do usuario real
if [ ! -f "$SSH_KEY_PRIV" ] || [ ! -f "$SSH_KEY_PUB" ]; then
    echo -e "${RED}❌ Non se atopan as claves SSH en $REAL_HOME/.ssh/${NC}"
    echo -e "${YELLOW}Claves esperadas:${NC}"
    echo -e "  - $SSH_KEY_PRIV"
    echo -e "  - $SSH_KEY_PUB"
    echo -e "\n${YELLOW}Executa primeiro o Paso 3 (Clave SSH) como usuario $REAL_USER${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Verificacións previas correctas${NC}\n"

# Solicitar o usuario destino
echo -e "${BLUE}Indica o usuario dos clientes ao que copiar a clave:${NC}"
read -p "Nome do usuario (default: usuario): " SSH_USER
SSH_USER=${SSH_USER:-usuario}

echo -e "\n${BLUE}Executando playbook de distribución...${NC}\n"

# Exportar variables de entorno
export SSH_USER
export SSH_KEY_PRIV
export SSH_KEY_PUB

# Se se executa con sudo, executar como usuario normal (non root)
# Se se executa como usuario normal, executar directamente
if [ $USING_SUDO -eq 1 ]; then
    # Executado con sudo: executar como usuario normal con su
    su "$REAL_USER" -c "ansible-playbook -i '$INVENTORY' '$PLAYBOOK' -b -K -e 'ssh_key_priv=$SSH_KEY_PRIV' -e 'ssh_key_pub=$SSH_KEY_PUB' -e 'ssh_user=$SSH_USER'"
    RESULTADO=$?
else
    # Executado como usuario normal: executar directamente
    # Pero necesita sudo para -b (become)
    ansible-playbook -i "$INVENTORY" "$PLAYBOOK" -b -K -e "ssh_key_priv=$SSH_KEY_PRIV" -e "ssh_key_pub=$SSH_KEY_PUB" -e "ssh_user=$SSH_USER"
    RESULTADO=$?
fi

# Verificar resultado
if [ $RESULTADO -eq 0 ]; then
    echo -e "\n${GREEN}╔════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║ ✅ Clave SSH distribuída correctamente aos clientes ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════╝${NC}"
    exit 0
else
    echo -e "\n${RED}❌ Erro ao distribuír a clave SSH${NC}"
    echo -e "${YELLOW}Revisa a saída anterior para máis detalles${NC}"
    exit 1
fi
