#!/bin/bash
# empaquetar_actualizacion.sh — xera o paquete de actualización en quente de
# Piztu que hai que subir a CADA Release de piztusistemas/Piztu, xunto ao
# instalador:
#
#   piztu_<versión>.zip          (contén só o executable "piztu")
#   piztu_<versión>.zip.sha256   (formato sha256sum)
#
# Sen estes dous ficheiros, Piztu avisa da versión nova pero só pode ofrecer
# descargar o instalador (ver piztu/internal/actualizacion). Coa Release
# 26.8–26.10.1 subiuse só o instalador e ningunha aula se actualizou só.
#
# Uso:  ./empaquetar_actualizacion.sh <binario-piztu> [cartafol-saída]
#   ex: ./empaquetar_actualizacion.sh files/piztu build/bin
# Despois:
#   gh release upload <versión> -R piztusistemas/Piztu \
#       build/bin/piztu_<versión>.zip build/bin/piztu_<versión>.zip.sha256
set -euo pipefail

BIN=${1:?Indica o binario de piztu (ex.: files/piztu)}
SAIDA=${2:-build/bin}
[ -x "$BIN" ] || { echo "Non é un executable: $BIN" >&2; exit 1; }

# A versión sae do propio binario (o VERSION que leva embebido), para que o
# nome do .zip nunca poida discrepar do que o binario di ser.
VER=$(cat "$(dirname "$0")/VERSION" | tr -d '[:space:]')
# grep -c e non grep -q: con pipefail, -q corta a tubaría e strings morre
# por SIGPIPE, o que facía fallar a comprobación aínda coa versión presente.
[ "$(strings -n 4 "$BIN" | grep -cF "$VER")" -gt 0 ] || { echo "O binario non contén a versión $VER do ficheiro VERSION" >&2; exit 1; }

mkdir -p "$SAIDA"
SAIDA=$(cd "$SAIDA" && pwd)
ZIP="$SAIDA/piztu_${VER}.zip"
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
install -m 755 "$BIN" "$TMP/piztu"
rm -f "$ZIP"
(cd "$TMP" && zip -q -X "$ZIP" piztu)
(cd "$SAIDA" && sha256sum "piztu_${VER}.zip" > "piztu_${VER}.zip.sha256")
echo "✅ $ZIP"
echo "✅ $ZIP.sha256  ($(cut -d' ' -f1 "$ZIP.sha256"))"
