# salt/avisoRuido.sls
# Espello de playbooks/avisoRuido.yaml — aviso emerxente de ruído excesivo.
# Aplícase con:  salt <host> state.apply avisoRuido

mostrar_aviso_ruido:
  cmd.run:
    - name: |
        export DISPLAY=:0
        export XAUTHORITY=/home/usuario/.Xauthority

        cat > /tmp/aviso_ruido.html << 'HTMLEOF'
        <!DOCTYPE html>
        <html lang="gl">
        <head>
          <meta charset="UTF-8">
          <meta http-equiv="refresh" content="6;url=about:blank">
          <title>Aviso</title>
          <style>
            * { margin: 0; padding: 0; box-sizing: border-box; }
            body {
              background: #1a1a2e;
              display: flex;
              flex-direction: column;
              align-items: center;
              justify-content: center;
              height: 100vh;
              font-family: 'Segoe UI', sans-serif;
              overflow: hidden;
            }
            .cabeceiro {
              position: absolute;
              top: 0; left: 0; right: 0;
              background: #16213e;
              padding: 10px 24px;
              display: flex;
              align-items: center;
              gap: 12px;
              border-bottom: 2px solid #e53e3e;
            }
            .cabeceiro .logo {
              color: white;
              font-size: 20px;
              font-weight: bold;
              letter-spacing: 1px;
            }
            .cabeceiro .logo span { color: #e53e3e; }
            .cabeceiro .url {
              color: #a0aec0;
              font-size: 12px;
            }
            .tarxeta {
              background: #16213e;
              border: 2px solid #e53e3e;
              border-radius: 16px;
              padding: 40px 60px;
              text-align: center;
              box-shadow: 0 0 40px rgba(229,62,62,0.4);
              animation: pulsar 1s ease-in-out infinite alternate;
              max-width: 520px;
            }
            @keyframes pulsar {
              from { box-shadow: 0 0 20px rgba(229,62,62,0.3); }
              to   { box-shadow: 0 0 50px rgba(229,62,62,0.7); }
            }
            .icona { font-size: 64px; margin-bottom: 16px; }
            .titulo {
              color: #e53e3e;
              font-size: 26px;
              font-weight: bold;
              margin-bottom: 12px;
              text-transform: uppercase;
              letter-spacing: 2px;
            }
            .mensaxe {
              color: #e2e8f0;
              font-size: 16px;
              line-height: 1.6;
              margin-bottom: 20px;
            }
            .aviso {
              color: #f6ad55;
              font-size: 13px;
              font-weight: bold;
            }
            .barra-tempo {
              margin-top: 24px;
              height: 4px;
              background: #2d3748;
              border-radius: 2px;
              overflow: hidden;
            }
            .barra-progreso {
              height: 100%;
              background: #e53e3e;
              border-radius: 2px;
              animation: baleirar 6s linear forwards;
            }
            @keyframes baleirar {
              from { width: 100%; }
              to   { width: 0%; }
            }
          </style>
        </head>
        <body>
          <div class="cabeceiro">
            <div class="logo">Piztu<span>.org</span></div>
            <div class="url">piztu.org — Xestión da aula de informática</div>
          </div>
          <div class="tarxeta">
            <div class="icona">🔊</div>
            <div class="titulo">⚠️ Demasiado Ruído</div>
            <div class="mensaxe">
              Baixade o volume da aula.<br>
              Se o ruído continúa, os equipos <strong>bloquearanse automaticamente</strong>.
            </div>
            <div class="aviso">Esta xanela pecharase en 6 segundos</div>
            <div class="barra-tempo"><div class="barra-progreso"></div></div>
          </div>
          <script>setTimeout(function(){ window.close(); }, 6000);</script>
        </body>
        </html>
        HTMLEOF

        if command -v chromium-browser &>/dev/null; then
          sudo -u usuario chromium-browser \
            --app=file:///tmp/aviso_ruido.html \
            --window-size=600,400 \
            --window-position=660,340 \
            --disable-extensions \
            --no-first-run \
            --noerrdialogs &
        elif command -v chromium &>/dev/null; then
          sudo -u usuario chromium \
            --app=file:///tmp/aviso_ruido.html \
            --window-size=600,400 \
            --window-position=660,340 \
            --disable-extensions \
            --no-first-run \
            --noerrdialogs &
        elif command -v firefox &>/dev/null; then
          sudo -u usuario firefox \
            --new-window file:///tmp/aviso_ruido.html &
        else
          # LC_ALL=C.UTF-8: sudo -u non conserva LC_CTYPE (queda en "C"), e
          # sen isto zenity rexeita calquera tilde/acento no texto.
          sudo -u usuario LC_ALL=C.UTF-8 zenity \
            --info \
            --title="Piztu.org — Aviso de Ruído" \
            --text="⚠️ DEMASIADO RUÍDO NA AULA\n\nBaixade o volume ou os equipos bloquearanse." \
            --timeout=6 &
        fi
    - shell: /bin/bash
