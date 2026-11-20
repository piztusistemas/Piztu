@echo off
rem Dobre clic: pide permisos de administrador e executa o preparador de Piztu.
rem (Garda este .bat e o .ps1 no mesmo cartafol, p.ex. nunha memoria USB.)
net session >nul 2>&1
if %errorlevel% neq 0 (
  powershell -NoProfile -Command "Start-Process -Verb RunAs -FilePath '%~f0'"
  exit /b
)
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0preparar_cliente_windows.ps1" %*
echo.
pause
