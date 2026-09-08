package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// StepUpChallengeProps carries the server-projected step-up challenge for
// one sensitive action. ActionLabel names the bound action and ReasonDetail
// the server's reason; ChallengeHref is its completion destination. The
// prompt authorizes nothing itself: completing the challenge is a server
// decision, and dismissing the prompt merely hides it for the page load.
type StepUpChallengeProps struct {
	I18nProps
	ActionLabel   string
	ReasonDetail  string
	ChallengeHref string
	Navigate      func(string)
}

// StepUpChallenge renders the risk-bound step-up prompt: which action needs
// elevated assurance, why, and where to complete it. Challenge destinations
// pass through the shared shell-destination policy, so a compromised
// server answer cannot turn the prompt into an attack surface.
func StepUpChallenge(props StepUpChallengeProps) ui.Node {
	dismissed := ui.UseState(false)
	if dismissed.Get() {
		return html.Fragment()
	}
	href := props.ChallengeHref
	if !validRecoveryHref(href) {
		href = ""
	}
	dismissLabel := props.Text("step_up.dismiss")
	actions := []ui.Node{}
	if href != "" {
		actions = append(actions, softwareLink(props.Navigate, html.Props{Class: "step-up-challenge"}, href, ui.Text(props.Text("step_up.challenge"))))
	}
	actions = append(actions, html.Button(html.Props{
		ID: "step-up-dismiss", Class: "step-up-dismiss", Type: "button",
		Aria:    map[string]string{"label": dismissLabel},
		OnClick: ui.UseEvent(func(ui.MouseEvent) { dismissed.Set(true) }),
	}, ui.Text(dismissLabel)))
	return html.Section(html.Props{ID: "step-up-challenge", Class: "step-up-challenge", Aria: map[string]string{"labelledby": "step-up-title"}},
		html.H2(html.Props{ID: "step-up-title", Class: "step-up-title"}, ui.Text(props.Text("step_up.title"))),
		html.P(html.Props{Class: "step-up-action"}, ui.Text(props.ActionLabel)),
		html.P(html.Props{Class: "step-up-reason"}, ui.Text(props.ReasonDetail)),
		html.Div(html.Props{Class: "step-up-actions"}, actions...),
	)
}

func stepUpChallenge(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	if view.StepUpChallenge == nil {
		return html.Fragment()
	}
	props := *view.StepUpChallenge
	props.I18nProps = I18nProps{Locale: view.Locale}
	props.Navigate = view.Navigate
	return ui.CreateElement(StepUpChallenge, props)
}
