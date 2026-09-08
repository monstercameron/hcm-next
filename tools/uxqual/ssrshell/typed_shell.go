package ssrshell

import (
	gwccss "github.com/monstercameron/GoWebComponents/v5/css"
)

// shellTypedStylesheet builds ShellCSS from typed declarations. Rule order
// matches the original const (single rule); the canonical serializer sorts
// declarations alphabetically and appends trailing semicolons.
func shellTypedStylesheet() string {
	return buildTypedSheet(declareShellStyles)
}

func declareShellStyles() {
	declareGlobal(`.visually-hidden`,
		gwccss.Position.Absolute,
		gwccss.W(gwccss.Px(1)),
		gwccss.H(gwccss.Px(1)),
		gwccss.Padding(gwccss.Zero),
		gwccss.Margin(gwccss.Px(-1)),
		gwccss.Raw("overflow", "hidden"),
		gwccss.Raw("clip", "rect(0,0,0,0)"),
		gwccss.Raw("white-space", "nowrap"),
		gwccss.Raw("border", "0"),
	)
}
