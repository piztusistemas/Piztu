.PHONY: build clean deps check-go check-wails files help

# BINARY: nome de ficheiro que produce "wails build" (fixado en wails.json,
# outputfilename). DIST_BINARY: nome final do executable distribuído — SEN
# versión no nome (installer/main.go xa a embebe con go:embed VERSION e a
# amosa dentro do propio asistente); un nome fixo evita ter que buscar cal é
# o máis novo.
BINARY = piztu-installer
DIST_BINARY = Piztu_Debian_installer

## build: Compila o instalador (require Go, wails e node/npm)
## Fronte da propia interface do instalador (frontend/) — non confundir con
## "files", que empaqueta o Piztu que se instala.
build: check-go check-wails assets/logo.png build/appicon.png files
	go mod tidy
	wails build
	@mv build/bin/$(BINARY) build/bin/$(DIST_BINARY)
	@echo ""
	@echo "✅ Compilado: ./installer/build/bin/$(DIST_BINARY)"
	@echo "   Executa con:  sudo ./installer/build/bin/$(DIST_BINARY)"

## assets/logo.png: Copia o logo dende o directorio pai
## Depende de ../logo.png (non abondaba con "assets/logo.png:" sen prerequisito:
## unha vez existise o ficheiro, Make xa non volvía copialo aínda que ../logo.png
## cambiase despois — por iso o logo vello (a estrela) quedaba atascado aquí).
assets/logo.png: ../logo.png
	@mkdir -p assets
	@cp ../logo.png assets/logo.png

## build/appicon.png: icona da xanela/taskbar de Wails e logo do frontend
## (mesma orixe ca assets/logo.png, ver nota de enriba).
build/appicon.png: ../logo.png
	@mkdir -p build frontend/src/assets/images
	@cp ../logo.png build/appicon.png
	@cp ../logo.png frontend/src/assets/images/logo.png

## files: Compila piztu e xunta en files/ só o necesario en tempo de execución.
## piztu substitúe main.py/app.py coma interface (inclúe o monitor de ruído
## integrado). Xesta xa NON fala coa API de Flask: hoxe é a súa propia app
## Wails (ver xesta/) e fala con piztu/internal/api vía piztuclient (ver
## docs/api-modulos.md) — piztu xa non arrinca "python3 app.py" el só.
## app.py/core/blueprints/templates/static/logos séguense empaquetando
## igualmente, pero só para uso manual/local de app.py (ver CLAUDE.md).
##
## piztu (o núcleo) pasou de Wails v2 a v3 — xa NON se compila con
## "wails build" (CLI de v2, incompatible co seu go.mod/main.go actuais).
## Non hai Taskfile.yml en piztu/piztu, así que tampouco vale "wails3 build":
## o xeito que funciona é o mesmo de sempre en dous pasos — build do
## frontend (Vite) e despois "go build -tags gtk3" (o webview GTK4 por
## defecto de Wails v3 non compila en Debian 12, so ten GTK 4.8; -tags gtk3
## conmuta ao backend GTK3). O propio instalador (esta app) segue en v2, sen
## cambios — check-wails de enriba sigue facendo falta para ISE build.
files:
	@rm -rf files
	@mkdir -p files/playbooks files/salt files/idiomas files/systemd
	@cd ../piztu/frontend && npm install && npm run build
	@cd ../piztu && go build -tags gtk3 -o build/bin/piztu .
	@cd ../piztu && go build -o build/bin/tao-helper ./cmd/taohelper
	@cd ../piztu && go build -o build/bin/historial-daemon ./cmd/historialdaemon
	@cp ../piztu/build/bin/piztu files/piztu
	@cp ../piztu/build/bin/tao-helper files/tao-helper
	@cp ../piztu/build/bin/historial-daemon files/historial-daemon
	@cp ../systemd/piztu-historial.service files/systemd/piztu-historial.service
	@cp ../config.yaml ../hosts ../ansible.cfg ../logo.png files/
	@cp ../lanzar_piztu.sh ../distribuirClave.sh ../xerar_inventario.sh files/
	@cp -r ../playbooks/. files/playbooks/
	@cp -r ../salt/. files/salt/
	@cp -r ../idiomas/. files/idiomas/
	@# app.py e as súas dependencias — xa non as usa Xesta (fala con
	@# piztu/internal/api), quedan só para uso manual/local de app.py.
	@# Sen __pycache__: son cachés compiladas da máquina de orixe, non fai
	@# falla empaquetalas.
	@cp ../app.py files/app.py
	@rsync -a --exclude='__pycache__' ../core/ files/core/
	@rsync -a --exclude='__pycache__' ../blueprints/ files/blueprints/
	@rsync -a ../templates/ files/templates/
	@rsync -a ../static/ files/static/
	@rsync -a ../logos/ files/logos/
	@chmod +x files/piztu files/tao-helper files/historial-daemon files/lanzar_piztu.sh files/distribuirClave.sh files/xerar_inventario.sh
	@# Nunca empaquetar un contrasinal real: fica en branco no molde e énchese
	@# no Paso 6 do instalador (LAMP+File Browser) co que introduza cada profesor.
	@sed -i 's/^\(\s*admin_contrasinal:\).*/\1 ""/' files/config.yaml
	@echo "✅ piztu compilado e ficheiros de Piztu (+ app.py para uso manual) copiados a installer/files/"

## deps: Instala as dependencias de sistema necesarias para compilar
deps:
	sudo apt-get update
	sudo apt-get install -y \
		golang-go \
		nodejs \
		npm \
		gcc \
		pkg-config \
		libgtk-3-dev \
		libwebkit2gtk-4.0-dev
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
	@echo ""
	@echo "✅ Dependencias instaladas. Agora executa: make build"

## clean: Elimina o binario compilado e ficheiros temporais
clean:
	rm -rf build/bin
	rm -f assets/logo.png build/appicon.png frontend/src/assets/images/logo.png
	rm -rf frontend/dist frontend/wailsjs
	rm -rf files

check-go:
	@which go > /dev/null 2>&1 || (echo "❌ Go non está instalado. Executa: make deps" && exit 1)

check-wails:
	@which wails > /dev/null 2>&1 || (echo "❌ wails non está instalado. Executa: go install github.com/wailsapp/wails/v2/cmd/wails@latest" && exit 1)

help:
	@echo ""
	@echo "Instalador de Piztu — Comandos dispoñibles:"
	@echo ""
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""
