// Comando historial-daemon — mantén o historial de sesións (quen usou cada
// equipo e cando, ver internal/historial) completándose aínda que ninguén
// teña Piztu aberto. Arrincado coma servizo systemd sempre activo (ver
// systemd/piztu-historial.service e instalarServizoHistorial en
// installer/install.go), non coma parte de piztu: se piztu tamén o
// fixese mentres a xanela está aberta, habería dous procesos escribindo o
// mesmo ficheiro diario ao mesmo tempo.
//
// Non fai máis ca conectar dúas pezas xa existentes: internal/tao.Monitor
// (polling de dixitalizacion/sesions/, cada 10s) chamando a
// internal/historial.Rastrexador.Procesar en cada volta.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"piztu/internal/config"
	"piztu/internal/historial"
	"piztu/internal/tao"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "historial-daemon: erro cargando a configuración:", err)
		os.Exit(1)
	}

	rastrexador := historial.Novo(cfg)
	mon := tao.Novo(cfg, tao.Eventos{OnEstado: rastrexador.Procesar})
	mon.Iniciar()

	fmt.Println("historial-daemon: rexistrando sesións en", cfg.BaseDir)

	sinal := make(chan os.Signal, 1)
	signal.Notify(sinal, syscall.SIGTERM, syscall.SIGINT)
	<-sinal

	mon.Detener()
}
