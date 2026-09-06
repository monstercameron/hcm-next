package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func appearancePage(view View) ui.Node {
	editable := len(view.EffectivePermissions) == 0 || view.Can(PageAppearance, "update")
	return ui.CreateElement(AppearancePage, AppearancePageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Theme:     view.Appearance, ColorModes: localizedColorModeOptions(view.Locale), Palettes: PaletteOptions(), Shapes: ShapeOptions(),
		Densities: DensityOptions(), Glyphs: GlyphOptions(), Typefaces: TypefaceOptions(),
		Navigation: NavigationOptions(), Motions: MotionOptions(),
		Editable: editable, OnPreview: view.PreviewTheme, OnSave: view.SaveTheme, OnReset: view.ResetTheme,
	})
}

func localizedColorModeOptions(locale LocaleContext) []AppearanceOption {
	options := ColorModeOptions()
	for index := range options {
		options[index].Label = locale.Text("appearance.color_mode_" + options[index].ID)
		options[index].Description = locale.Text("appearance.color_mode_" + options[index].ID + "_help")
	}
	return options
}
