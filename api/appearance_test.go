package portainer

import "testing"

func TestDefaultAppearance(t *testing.T) {
	t.Parallel()

	got := DefaultAppearance()
	if got.Branding.Name != "Portainer" || got.Branding.FaviconURL != "" {
		t.Fatalf("unexpected branding defaults: %+v", got.Branding)
	}
	if got.Typography.FontFamily != "inter" || got.Typography.FontScale != 100 {
		t.Fatalf("unexpected typography defaults: %+v", got.Typography)
	}
	if got.Layout.SidebarWidth != 300 || got.Layout.SidebarDefault != "open" || got.Layout.Density != "standard" {
		t.Fatalf("unexpected layout defaults: %+v", got.Layout)
	}
	if got.DefaultTheme != "auto" {
		t.Fatalf("unexpected default theme: %q", got.DefaultTheme)
	}
}

func TestEffectiveAppearanceUsesDefaultsForLegacySettings(t *testing.T) {
	t.Parallel()

	settings := &Settings{}
	if got, want := settings.EffectiveAppearance(), DefaultAppearance(); got != want {
		t.Fatalf("EffectiveAppearance() = %+v, want defaults %+v", got, want)
	}
}

func TestAppearanceSettingsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*AppearanceSettings)
		wantErr bool
	}{
		{name: "defaults"},
		{name: "valid URLs and colors", mutate: func(a *AppearanceSettings) {
			a.Branding.Name = "Acme Platform"
			a.Branding.FaviconURL = "https://assets.example.test/favicon.png"
			a.Colors.Light.Accent = "#326CE5"
			a.Colors.Dark.PageBackground = "#121212"
			a.Typography.FontFamily = "system"
			a.Typography.FontScale = 120
			a.Layout.SidebarWidth = 360
			a.Layout.SidebarDefault = "closed"
			a.Layout.Density = "compact"
			a.DefaultTheme = "highcontrast"
		}},
		{name: "same origin favicon", mutate: func(a *AppearanceSettings) { a.Branding.FaviconURL = "/custom/favicon.svg" }},
		{name: "brand name limit", mutate: func(a *AppearanceSettings) { a.Branding.Name = string(make([]byte, 81)) }, wantErr: true},
		{name: "color format", mutate: func(a *AppearanceSettings) { a.Colors.Light.Accent = "red" }, wantErr: true},
		{name: "protocol relative favicon", mutate: func(a *AppearanceSettings) { a.Branding.FaviconURL = "//example.test/favicon.ico" }, wantErr: true},
		{name: "javascript favicon", mutate: func(a *AppearanceSettings) { a.Branding.FaviconURL = "javascript:alert(1)" }, wantErr: true},
		{name: "font family", mutate: func(a *AppearanceSettings) { a.Typography.FontFamily = "Comic Sans" }, wantErr: true},
		{name: "font scale", mutate: func(a *AppearanceSettings) { a.Typography.FontScale = 130 }, wantErr: true},
		{name: "sidebar width below minimum", mutate: func(a *AppearanceSettings) { a.Layout.SidebarWidth = 239 }, wantErr: true},
		{name: "sidebar width above maximum", mutate: func(a *AppearanceSettings) { a.Layout.SidebarWidth = 361 }, wantErr: true},
		{name: "sidebar default", mutate: func(a *AppearanceSettings) { a.Layout.SidebarDefault = "floating" }, wantErr: true},
		{name: "density", mutate: func(a *AppearanceSettings) { a.Layout.Density = "dense" }, wantErr: true},
		{name: "default theme", mutate: func(a *AppearanceSettings) { a.DefaultTheme = "sepia" }, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			appearance := DefaultAppearance()
			if tt.mutate != nil {
				tt.mutate(&appearance)
			}
			err := appearance.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}
