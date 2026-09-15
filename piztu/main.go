package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

// nomeXanelaPrincipal é o Name da única xanela nativa de Piztu (v3 permite
// varias WebviewWindow no mesmo proceso, pero Piztu só usa unha - ver
// exploración previa: sen menú nativo, bandexa, xanela sen marco nin
// segunda xanela propia; os módulos externos - Tao/Yang/Xesta - son
// procesos independentes lanzados con exec.Command, non outra xanela deste
// mesmo proceso).
const nomeXanelaPrincipal = "piztu"

func main() {
	app := NewApp()

	wailsApp := application.New(application.Options{
		Name: "Piztu",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Icon: icon,
		Linux: application.LinuxOptions{
			ProgramName: "Piztu",
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             nomeXanelaPrincipal,
		Title:            "Piztu",
		Width:            1280,
		Height:           820,
		StartState:       application.WindowStateMaximised,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		println("Error:", err.Error())
	}
}
