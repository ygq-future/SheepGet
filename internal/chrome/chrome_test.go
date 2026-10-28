package chrome

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"sheep-get/internal/config"
)

func TestNativeWindowThemeDark(t *testing.T) {
	cases := []struct {
		name       string
		mode       config.ThemeMode
		systemDark bool
		want       bool
	}{
		{"explicit dark", config.ThemeDark, false, true},
		{"explicit light", config.ThemeLight, true, false},
		{"system dark when true", config.ThemeSystem, true, true},
		{"system dark when false", config.ThemeSystem, false, false},
		{"unknown defaults to systemDark", config.ThemeMode("other"), true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NativeWindowThemeDark(c.mode, c.systemDark)
			if got != c.want {
				t.Errorf("NativeWindowThemeDark(%v, %v) = %v, want %v", c.mode, c.systemDark, got, c.want)
			}
		})
	}
}

func TestNativeWindowTheme(t *testing.T) {
	if NativeWindowTheme(config.ThemeDark, false) != application.Dark {
		t.Errorf("expected Dark theme")
	}
	if NativeWindowTheme(config.ThemeLight, true) != application.Light {
		t.Errorf("expected Light theme")
	}
	if NativeWindowTheme(config.ThemeSystem, true) != application.Dark {
		t.Errorf("expected Dark theme when system is dark")
	}
	if NativeWindowTheme(config.ThemeSystem, false) != application.Light {
		t.Errorf("expected Light theme when system is light")
	}
}

func TestImmersiveWindowChrome(t *testing.T) {
	frameless, translucent, windows, mac := ImmersiveWindowChrome("windows")
	if !frameless || translucent || !windows.WebView2CompositionHosting || windows.BackdropType != application.None {
		t.Errorf("windows chrome invalid: %v %v %+v", frameless, translucent, windows)
	}
	if mac.TitleBar != (application.MacTitleBar{}) {
		t.Errorf("windows should not touch mac titlebar")
	}

	frameless, translucent, windows, mac = ImmersiveWindowChrome("darwin")
	if frameless || !translucent || windows.WebView2CompositionHosting || mac.Backdrop != application.MacBackdropTranslucent {
		t.Errorf("darwin chrome invalid: %v %v %+v %+v", frameless, translucent, windows, mac)
	}

	frameless, translucent, windows, mac = ImmersiveWindowChrome("linux")
	if frameless || translucent || windows.WebView2CompositionHosting || mac.Backdrop != 0 {
		t.Errorf("linux chrome should remain standard: %v %v %+v %+v", frameless, translucent, windows, mac)
	}
}
