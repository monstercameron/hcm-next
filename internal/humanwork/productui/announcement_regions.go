package productui

// Announcement is one message the announcements section may
// present. Assertive claims the single assertive live region;
// everything else belongs to a polite region. Messages pass
// through untouched — region membership is the governance,
// never flag rewriting.
type Announcement struct {
	ID        string
	Message   string
	Assertive bool
}

// GovernAnnouncements splits one announcement stream into the
// polite region and the assertive region in admission order.
// The first assertive claim wins; later assertive claims join
// the polite region so assistive technology never receives
// two assertive announcements from one surface.
func GovernAnnouncements(items []Announcement) (polite, assertive []Announcement) {
	polite = make([]Announcement, 0, len(items))
	assertive = make([]Announcement, 0, 1)
	claimed := false
	for _, item := range items {
		if item.Assertive && !claimed {
			claimed = true
			assertive = append(assertive, item)
			continue
		}
		polite = append(polite, item)
	}
	return polite, assertive
}
