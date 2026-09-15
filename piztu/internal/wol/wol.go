// Package wol envía o "paquete máxico" de Wake-on-LAN de forma nativa, sen
// depender do binario `wakeonlan` (que en macOS non vén de serie). Un equipo
// apagado non ten SSH nin minion vivo, así que a única forma de acendelo é
// emitir por broadcast UDP o paquete máxico coa súa MAC.
package wol

import (
	"fmt"
	"net"
	"syscall"
)

// Enviar constrúe e emite o paquete máxico WoL para `mac` por broadcast global
// (255.255.255.255:9). Activa SO_BROADCAST no socket para que o SO permita o
// envío a un enderezo de difusión (necesario en Linux e macOS).
func Enviar(mac string) error {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("MAC non válida %q: %w", mac, err)
	}

	// Paquete máxico: 6 bytes 0xFF seguidos de 16 repeticións do MAC (102 bytes).
	pkt := make([]byte, 6, 102)
	for i := range pkt {
		pkt[i] = 0xFF
	}
	for i := 0; i < 16; i++ {
		pkt = append(pkt, hw...)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("WoL: abrindo socket: %w", err)
	}
	defer conn.Close()

	// Activar broadcast no descritor subxacente (SO_BROADCAST).
	rc, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("WoL: %w", err)
	}
	var setErr error
	if err := rc.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); err != nil {
		return fmt.Errorf("WoL: control do socket: %w", err)
	}
	if setErr != nil {
		return fmt.Errorf("WoL: activando broadcast: %w", setErr)
	}

	destino := &net.UDPAddr{IP: net.IPv4bcast, Port: 9}
	if _, err := conn.WriteToUDP(pkt, destino); err != nil {
		return fmt.Errorf("WoL: enviando paquete: %w", err)
	}
	return nil
}
