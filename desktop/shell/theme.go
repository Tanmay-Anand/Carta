// Package shell is Carta Desktop's window: the top bar, the theme, and the
// registry that future tools plug into.
package shell

import (
	_ "embed"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Two bundled families, the same files on every surface Carta draws
// (docs/DESIGN_SYSTEM_CARTA.md §2.2). Inter (OFL, fonts/Inter-OFL.txt) carries
// prose and controls, Light for the headline. JetBrains Mono (OFL,
// fonts/JetBrainsMono-OFL.txt) carries everything measured; it is the variable
// font the Android app ships, rendered at its default weight.
var (
	//go:embed fonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed fonts/Inter-Light.ttf
	interLight []byte
	//go:embed fonts/JetBrainsMono.ttf
	monoTTF []byte

	Sans      = fyne.NewStaticResource("Inter-Regular.ttf", interRegular)
	SansLight = fyne.NewStaticResource("Inter-Light.ttf", interLight)
	Mono      = fyne.NewStaticResource("JetBrainsMono.ttf", monoTTF)
)

// Palette is design D's colour tokens for one variant
// (docs/DESIGN_SYSTEM_CARTA.md §2.1). Colour means state, never decoration.
type Palette struct {
	BG, Panel, FG, Dim, Hair, Thread, Red, Amber, Warn color.Color
	// Faint is decoration only: never text, never the only carrier of a
	// meaning. It fails 4.5:1 against BG in both variants.
	Faint color.Color
}

var (
	Dark = Palette{
		BG: hex(0x000000), Panel: hex(0x0B0B0B), FG: hex(0xF0F0ED), Dim: hex(0x8B8B87),
		Faint: hex(0x4A4A47), Hair: hex(0x1D1D1C), Thread: hex(0x2A2A28),
		Red: hex(0xFF3B2F), Amber: hex(0xD29B3C), Warn: hex(0xE3A008),
	}
	Light = Palette{
		BG: hex(0xFAFAF8), Panel: hex(0xFFFFFF), FG: hex(0x111111), Dim: hex(0x62625E),
		Faint: hex(0xB4B4AE), Hair: hex(0xE4E4DF), Thread: hex(0xCFCFCA),
		Red: hex(0xD92D20), Amber: hex(0xA87A00), Warn: hex(0xB54708),
	}
)

// PaletteFor returns the tokens for a variant.
func PaletteFor(v fyne.ThemeVariant) Palette {
	if v == theme.VariantLight {
		return Light
	}
	return Dark
}

// Current returns the tokens for the variant on screen now.
func Current() Palette {
	return PaletteFor(fyne.CurrentApp().Settings().ThemeVariant())
}

// Theme maps Fyne's colour roles onto Carta's tokens. Every role a Carta
// screen can show is mapped: one left to Fyne's default brings its blue-grey
// back in a corner of the window.
type Theme struct{}

func (Theme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	p := PaletteFor(v)
	switch n {
	case theme.ColorNameBackground, theme.ColorNameInputBackground, theme.ColorNameButton,
		theme.ColorNameHeaderBackground:
		return p.BG
	case theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return p.Panel
	case theme.ColorNameForeground, theme.ColorNamePrimary:
		return p.FG
	case theme.ColorNameForegroundOnPrimary:
		return p.BG
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return p.Dim
	case theme.ColorNameSeparator, theme.ColorNameInputBorder, theme.ColorNameDisabledButton:
		return p.Hair
	case theme.ColorNameFocus:
		return p.Dim
	case theme.ColorNameSelection:
		return withAlpha(p.Dim, 0x40)
	case theme.ColorNameHover, theme.ColorNamePressed:
		return withAlpha(p.Dim, 0x18)
	case theme.ColorNameScrollBar:
		return withAlpha(p.Dim, 0x66)
	case theme.ColorNameScrollBarBackground:
		return color.Transparent
	// Depth comes from tone and hairlines, never shadow.
	case theme.ColorNameShadow:
		return color.Transparent
	case theme.ColorNameError:
		return p.Red
	case theme.ColorNameWarning:
		return p.Warn
	case theme.ColorNameSuccess, theme.ColorNameHyperlink:
		return p.FG
	}
	return theme.DefaultTheme().Color(n, v)
}

func (Theme) Font(s fyne.TextStyle) fyne.Resource {
	if s.Monospace {
		return Mono
	}
	// Design D has no bold: weight is carried by size and by Light against
	// Regular. A widget that asks for bold gets Regular.
	return Sans
}

func (Theme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

// Sizes the theme adds for Carta's own text roles, so a wrapping label can use
// them (widget.Label.SizeName).
const (
	SizeEvidence fyne.ThemeSizeName = "carta-evidence" // mono evidence lines, 11
	SizeSmall    fyne.ThemeSizeName = "carta-small"    // finding details, 13
)

func (Theme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case SizeEvidence:
		return 11
	case SizeSmall:
		return 13
	case theme.SizeNameText:
		return 14
	case theme.SizeNameInputRadius, theme.SizeNameSelectionRadius:
		return 0
	case theme.SizeNameInputBorder:
		return 1
	case theme.SizeNameScrollBar:
		return 6
	case theme.SizeNameScrollBarSmall:
		return 3
	}
	return theme.DefaultTheme().Size(n)
}

func hex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

func withAlpha(c color.Color, a uint8) color.NRGBA {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	n.A = a
	return n
}
