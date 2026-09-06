package balance

// EntryRepository is the persistence-neutral port already exercised by the
// kernel's in-memory EntryStore. A database adapter may add tenant and
// transaction concerns at its own boundary without making the balance rules
// depend on a driver or a database package.
type EntryRepository interface {
	Post(PostRequest, AccumulatorDefinition) (PostReceipt, error)
	Entries(string) []BalanceEntry
	Head(string) int64
}

var _ EntryRepository = (*EntryStore)(nil)
