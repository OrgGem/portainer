package portainer

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// AppearanceSettings contains only public presentation settings. LogoURL remains
// in Settings so that existing clients and installations retain their logo.
type AppearanceSettings struct {
	Branding     AppearanceBranding   `json:"Branding"`
	Colors       AppearanceColors     `json:"Colors"`
	Typography   AppearanceTypography `json:"Typography"`
	Layout       AppearanceLayout     `json:"Layout"`
	DefaultTheme string               `json:"DefaultTheme" enums:"auto,light,dark,highcontrast"`
}

type AppearanceBranding struct {
	Name       string `json:"Name" example:"Portainer"`
	FaviconURL string `json:"FaviconURL" example:"/favicon.png"`
}

type AppearanceColors struct {
	Light AppearancePalette `json:"Light"`
	Dark  AppearancePalette `json:"Dark"`
}

// Empty palette fields use the corresponding built-in theme color.
type AppearancePalette struct {
	Accent            string `json:"Accent" example:"#326CE5"`
	PageBackground    string `json:"PageBackground" example:"#F5F5F5"`
	SurfaceBackground string `json:"SurfaceBackground" example:"#FFFFFF"`
	SidebarBackground string `json:"SidebarBackground" example:"#2E2F33"`
}

type AppearanceTypography struct {
	FontFamily string `json:"FontFamily" enums:"inter,system,arial"`
	FontScale  int    `json:"FontScale" enums:"100,110,120"`
}

type AppearanceLayout struct {
	SidebarWidth   int    `json:"SidebarWidth" minimum:"240" maximum:"360"`
	SidebarDefault string `json:"SidebarDefault" enums:"open,closed"`
	Density        string `json:"Density" enums:"standard,compact"`
}

func DefaultAppearance() AppearanceSettings {
	return AppearanceSettings{
		Branding:     AppearanceBranding{Name: "Portainer"},
		Typography:   AppearanceTypography{FontFamily: "inter", FontScale: 100},
		Layout:       AppearanceLayout{SidebarWidth: 300, SidebarDefault: "open", Density: "standard"},
		DefaultTheme: "auto",
	}
}

// EffectiveAppearance preserves the original presentation for existing data.
func (s *Settings) EffectiveAppearance() AppearanceSettings {
	if s.Appearance == nil {
		return DefaultAppearance()
	}
	return *s.Appearance
}

var appearanceHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Validate rejects arbitrary CSS and verifies the complete settings object
// before it is written in the same transaction as LogoURL.
func (a AppearanceSettings) Validate() error {
	if !utf8.ValidString(a.Branding.Name) || utf8.RuneCountInString(a.Branding.Name) > 80 {
		return fmt.Errorf("brand name must contain at most 80 characters")
	}
	if !validAppearanceFavicon(a.Branding.FaviconURL) {
		return fmt.Errorf("favicon URL must be an HTTP(S) URL or a same-origin absolute path")
	}
	for _, palette := range []AppearancePalette{a.Colors.Light, a.Colors.Dark} {
		for _, color := range []string{palette.Accent, palette.PageBackground, palette.SurfaceBackground, palette.SidebarBackground} {
			if color != "" && !appearanceHexColor.MatchString(color) {
				return fmt.Errorf("appearance colors must be empty or #RRGGBB")
			}
		}
	}
	if a.Typography.FontFamily != "inter" && a.Typography.FontFamily != "system" && a.Typography.FontFamily != "arial" {
		return fmt.Errorf("appearance font must be inter, system, or arial")
	}
	if a.Typography.FontScale != 100 && a.Typography.FontScale != 110 && a.Typography.FontScale != 120 {
		return fmt.Errorf("appearance font scale must be 100, 110, or 120")
	}
	if a.Layout.SidebarWidth < 240 || a.Layout.SidebarWidth > 360 {
		return fmt.Errorf("appearance sidebar width must be between 240 and 360")
	}
	if a.Layout.SidebarDefault != "open" && a.Layout.SidebarDefault != "closed" {
		return fmt.Errorf("appearance sidebar default must be open or closed")
	}
	if a.Layout.Density != "standard" && a.Layout.Density != "compact" {
		return fmt.Errorf("appearance density must be standard or compact")
	}
	if a.DefaultTheme != "auto" && a.DefaultTheme != "light" && a.DefaultTheme != "dark" && a.DefaultTheme != "highcontrast" {
		return fmt.Errorf("appearance default theme must be auto, light, dark, or highcontrast")
	}
	return nil
}

func validAppearanceFavicon(value string) bool {
	if value == "" {
		return true
	}
	// Browsers treat backslashes as slashes for HTTP URLs; do not allow an
	// apparently relative path to become a protocol-relative external URL.
	if strings.Contains(value, `\`) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil {
		return false
	}
	if strings.HasPrefix(value, "/") {
		return !strings.HasPrefix(value, "//") && parsed.Host == "" && !parsed.IsAbs()
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}
