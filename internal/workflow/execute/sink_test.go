package execute

// inertRow is a no-op dbport.Row: its Scan always succeeds without writing
// to dest. It exists purely as a stand-in QueryRow result for this
// package's in-memory dbport.Tx test doubles (see memoryTx in
// resume_test.go), which never route through a real SQL driver and so have
// nothing meaningful to scan into a caller's destinations.
type inertRow struct{}

func (inertRow) Scan(dest ...any) error { return nil }
