package productui

// PersonalSummary is the derived-only rollup for one person
// present in an admitted work stream: counts of active and
// completed items. It adds no truth beyond the stream; counts
// reconcile item-for-item with the attention (WEB-098) and
// recent-work (WEB-099) lists.
type PersonalSummary struct {
	Person    string
	Active    int
	Completed int
}

// SummarizePersonal derives one summary per person present in
// an admitted work stream, in order of first appearance. The
// compiler presents without inventing persons, copying titles,
// or assigning priority.
func SummarizePersonal(items []WorkItem) []PersonalSummary {
	summaries := make([]PersonalSummary, 0, len(items))
	index := make(map[string]int, len(items))
	for _, item := range items {
		at, found := index[item.Person]
		if !found {
			at = len(summaries)
			index[item.Person] = at
			summaries = append(summaries, PersonalSummary{Person: item.Person})
		}
		if item.Terminal {
			summaries[at].Completed++
		} else {
			summaries[at].Active++
		}
	}
	return summaries
}
