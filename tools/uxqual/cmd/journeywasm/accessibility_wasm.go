//go:build js && wasm

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"

	"github.com/monstercameron/hcm-next/internal/humanwork/productui"
)

const accessibilityStorageVersion = "v1"

type browserAccessibilityController struct {
	key   string
	saved productui.AccessibilityPreferences
}

func newBrowserAccessibilityController(tenant, subject string) *browserAccessibilityController {
	scope := strings.ToLower(strings.TrimSpace(tenant + "\x00" + subject))
	if scope == "\x00" {
		scope = "current"
	}
	digest := sha256.Sum256([]byte(scope))
	controller := &browserAccessibilityController{key: fmt.Sprintf("hcmnext.accessibility.%s.%x", accessibilityStorageVersion, digest[:8])}
	controller.saved = controller.load()
	return controller
}

func (c *browserAccessibilityController) Saved() productui.AccessibilityPreferences {
	if c == nil {
		return productui.DefaultAccessibilityPreferences()
	}
	return productui.NormalizeAccessibilityPreferences(c.saved)
}

func (c *browserAccessibilityController) Preview(value productui.AccessibilityPreferences) {
	c.Apply(value)
	c.setStatus("preview", "preview")
}

func (c *browserAccessibilityController) Save(value productui.AccessibilityPreferences) {
	if c == nil {
		return
	}
	value = productui.NormalizeAccessibilityPreferences(value)
	c.saved = value
	c.Apply(value)
	stored := false
	if body, err := json.Marshal(value); err == nil {
		stored = writeLocalStorage(c.key, string(body))
	}
	if stored {
		c.setStatus("saved", "success")
	} else {
		c.setStatus("storage_unavailable", "warning")
	}
}

func (c *browserAccessibilityController) Reset() {
	if c == nil {
		return
	}
	c.saved = productui.DefaultAccessibilityPreferences()
	cleared := removeLocalStorage(c.key)
	c.Apply(c.saved)
	c.syncEditor(c.saved)
	if cleared {
		c.setStatus("reset", "success")
	} else {
		c.setStatus("reset_unavailable", "warning")
	}
}

func (c *browserAccessibilityController) Apply(value productui.AccessibilityPreferences) {
	value = productui.NormalizeAccessibilityPreferences(value)
	root := js.Global().Get("document").Get("documentElement")
	if !root.Truthy() {
		return
	}
	attributes := productui.AccessibilityPreferenceAttributes(value)
	for _, name := range []string{"data-hcm-text-size", "data-hcm-contrast", "data-hcm-motion-preference", "data-hcm-links"} {
		root.Call("setAttribute", name, attributes[name])
	}
}

func (c *browserAccessibilityController) load() productui.AccessibilityPreferences {
	defaults := productui.DefaultAccessibilityPreferences()
	raw, ok := readLocalStorage(c.key)
	if !ok || strings.TrimSpace(raw) == "" {
		return defaults
	}
	var value productui.AccessibilityPreferences
	if json.Unmarshal([]byte(raw), &value) != nil {
		return defaults
	}
	return productui.NormalizeAccessibilityPreferences(value)
}

func (c *browserAccessibilityController) syncEditor(value productui.AccessibilityPreferences) {
	values := map[string]string{"text-size": value.TextSize, "contrast": value.Contrast, "motion-preference": value.Motion, "links": value.Links}
	document := js.Global().Get("document")
	for name, selected := range values {
		inputs := document.Call("querySelectorAll", `input[name="`+name+`"]`)
		for index := 0; index < inputs.Get("length").Int(); index++ {
			input := inputs.Index(index)
			input.Set("checked", input.Get("value").String() == selected)
		}
	}
}

func (c *browserAccessibilityController) setStatus(code, tone string) {
	status := js.Global().Get("document").Call("getElementById", "accessibility-status")
	if !status.Truthy() {
		return
	}
	status.Set("textContent", accessibilityStatusMessage(code))
	status.Call("setAttribute", "data-tone", tone)
}

func accessibilityStatusMessage(code string) string {
	locale := js.Global().Get("document").Get("documentElement").Call("getAttribute", "lang").String()
	messages := map[string]map[string]string{
		"en-US": {"preview": "Previewing unsaved accessibility preferences", "saved": "Accessibility preferences saved for this browser", "storage_unavailable": "Preferences applied, but browser storage is unavailable", "reset": "Accessibility defaults restored", "reset_unavailable": "Defaults applied, but browser storage could not be cleared"},
		"de-DE": {"preview": "Nicht gespeicherte Einstellungen werden angezeigt", "saved": "Barrierefreiheitseinstellungen wurden für diesen Browser gespeichert", "storage_unavailable": "Einstellungen angewendet, Browserspeicher ist jedoch nicht verfügbar", "reset": "Standardeinstellungen wurden wiederhergestellt", "reset_unavailable": "Standards angewendet, Browserspeicher konnte jedoch nicht gelöscht werden"},
		"ar":    {"preview": "تتم معاينة تفضيلات إمكانية الوصول غير المحفوظة", "saved": "تم حفظ تفضيلات إمكانية الوصول لهذا المتصفح", "storage_unavailable": "تم تطبيق التفضيلات، لكن تخزين المتصفح غير متاح", "reset": "تمت استعادة إعدادات إمكانية الوصول الافتراضية", "reset_unavailable": "تم تطبيق الإعدادات الافتراضية، لكن تعذر مسح تخزين المتصفح"},
	}
	if localized, ok := messages[locale]; ok {
		if message := localized[code]; message != "" {
			return message
		}
	}
	return messages[productui.DefaultProductLocale][code]
}
