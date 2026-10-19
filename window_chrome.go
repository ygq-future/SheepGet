package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func immersiveWindowChrome(goos string) (frameless bool, translucent bool, windows application.WindowsWindow, mac application.MacWindow) {
	switch goos {
	case "windows":
		return true, true, application.WindowsWindow{BackdropType: application.Acrylic, WebView2CompositionHosting: true}, application.MacWindow{}
	case "darwin":
		return false, true, application.WindowsWindow{}, application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: true,
			},
			InvisibleTitleBarHeight: 0,
		}
	default:
		return false, false, application.WindowsWindow{}, application.MacWindow{}
	}
}
