package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func appearancePage(view View) ui.Node {
	return ui.CreateElement(AppearancePage, AppearancePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Theme:     view.Appearance, Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(),
		Navigation: NavigationOptions(), Motions: MotionOptions(),
		OnPreview: view.PreviewTheme, OnSave: view.SaveTheme, OnReset: view.ResetTheme,
	})
}
