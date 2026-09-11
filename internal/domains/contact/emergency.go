package contact

// Emergency-contact membership, order and privacy (CONF-018): one
// worker's ordered contact set with integer priority uniqueness,
// reachability, governed person-link provenance, minimum disclosure and
// relationship labels that grant no dependent, beneficiary or
// representative authority. ADD requires an empty ID and assigns one;
// REVISE, REMOVE, REORDER and CORRECT require exact IDs. Reorder names
// every member or loses none: partial reorder is refused. Ordered-set
// invariants are domain-owned; transport and provider adapters consume
// only normalized revisions.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrAddWithID reports an ADD carrying an ID: supplied IDs never
	// create, so add and replace cannot conflate.
	ErrAddWithID = errors.New("contact: add carries an id")

	// ErrMissingID reports a revise, remove, reorder or correct step
	// without the member ID it names.
	ErrMissingID = errors.New("contact: member id is required")

	// ErrUnknownMember reports an ID outside the current membership.
	ErrUnknownMember = errors.New("contact: unknown member")

	// ErrMemberLost reports a reorder that does not name every member.
	ErrMemberLost = errors.New("contact: reorder loses members")

	// ErrPriorityCollision reports two members sharing one priority.
	ErrPriorityCollision = errors.New("contact: priority collision")

	// ErrUnlinkedPerson reports treating a supplied person as canonical
	// without governed linkage.
	ErrUnlinkedPerson = errors.New("contact: person is not canonically linked")

	// ErrLabelAuthority reports reading dependent, beneficiary or
	// representative authority from a relationship label: labels grant
	// nothing, ever.
	ErrLabelAuthority = errors.New("contact: relationship label grants no authority")
)

// Relationship is the closed relationship vocabulary.
type Relationship string

const (
	RelationshipSpouse  Relationship = "SPOUSE"
	RelationshipPartner Relationship = "PARTNER"
	RelationshipParent  Relationship = "PARENT"
	RelationshipSibling Relationship = "SIBLING"
	RelationshipChild   Relationship = "CHILD"
	RelationshipFriend  Relationship = "FRIEND"
	RelationshipOther   Relationship = "OTHER"
)

// EmergencyContact is one ordered member. PersonName is display-only
// until LinkPerson binds PersonRef with evidence; only then is the person
// canonical.
type EmergencyContact struct {
	ID           string
	WorkerID     string
	PersonName   string
	Relationship Relationship
	Priority     int
	Reachability []string
	Notes        string
	PersonRef    string
	PersonLinked bool
}

// AuthorityGranted always reports none: relationship labels are meaning,
// never authority.
func (c EmergencyContact) AuthorityGranted() []string { return nil }

// AuthorityOf refuses any dependent, beneficiary or representative claim
// drawn from the label.
func (c EmergencyContact) AuthorityOf(kind string) error {
	switch strings.ToLower(kind) {
	case "dependent", "beneficiary", "representative":
		return fmt.Errorf("contact: %s label: %w", c.Relationship, ErrLabelAuthority)
	default:
		return fmt.Errorf("contact: unknown authority %q: %w", kind, ErrLabelAuthority)
	}
}

// IsCanonical reports whether the supplied person is canonically linked.
func (c EmergencyContact) IsCanonical() bool { return c.PersonLinked && c.PersonRef != "" }

// EmergencyContactSet is one worker's ordered set at one revision.
type EmergencyContactSet struct {
	WorkerID string
	Revision uint64
	Members  []EmergencyContact
	Digest   string
}

func setDigest(workerID string, revision uint64, members []EmergencyContact) string {
	parts := []string{"emergency-contacts", workerID, fmt.Sprintf("%d", revision)}
	ordered := append([]EmergencyContact(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	for _, member := range ordered {
		parts = append(parts, member.ID, string(member.Relationship), fmt.Sprintf("%d", member.Priority),
			strings.Join(member.Reachability, ","), member.PersonRef, fmt.Sprintf("%v", member.PersonLinked))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func indexByID(members []EmergencyContact) (map[string]int, error) {
	index := make(map[string]int, len(members))
	for i, member := range members {
		if member.ID == "" {
			return nil, ErrMissingID
		}
		if _, dup := index[member.ID]; dup {
			return nil, fmt.Errorf("contact: duplicate %s: %w", member.ID, ErrPriorityCollision)
		}
		index[member.ID] = i
	}
	priorities := make(map[int]bool, len(members))
	for _, member := range members {
		if member.Priority < 1 {
			return nil, fmt.Errorf("contact: %s priority: %w", member.ID, ErrPriorityCollision)
		}
		if priorities[member.Priority] {
			return nil, fmt.Errorf("contact: priority %d: %w", member.Priority, ErrPriorityCollision)
		}
		priorities[member.Priority] = true
	}
	return index, nil
}

func seal(set EmergencyContactSet) EmergencyContactSet {
	set.Digest = setDigest(set.WorkerID, set.Revision, set.Members)
	return set
}

// NewSet opens one empty set.
func NewSet(workerID string) EmergencyContactSet {
	return seal(EmergencyContactSet{WorkerID: workerID, Revision: 1})
}

// Add appends one member under a domain-assigned ID. The caller supplies
// no ID: missing IDs create, supplied IDs never do.
func Add(set EmergencyContactSet, contact EmergencyContact) (EmergencyContactSet, error) {
	if contact.ID != "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: Add: %w", ErrAddWithID)
	}
	if !validRelationship(contact.Relationship) {
		return EmergencyContactSet{}, fmt.Errorf("contact: Add: unknown relationship")
	}
	contact.ID = fmt.Sprintf("ec/%s/%d", set.WorkerID, len(set.Members)+1)
	contact.WorkerID = set.WorkerID
	members := append(append([]EmergencyContact(nil), set.Members...), contact)
	if _, err := indexByID(members); err != nil {
		return EmergencyContactSet{}, err
	}
	set.Members = members
	set.Revision++
	return seal(set), nil
}

func validRelationship(relationship Relationship) bool {
	switch relationship {
	case RelationshipSpouse, RelationshipPartner, RelationshipParent,
		RelationshipSibling, RelationshipChild, RelationshipFriend, RelationshipOther:
		return true
	default:
		return false
	}
}

// Revise replaces one member's values under its exact ID.
func Revise(set EmergencyContactSet, contact EmergencyContact) (EmergencyContactSet, error) {
	if contact.ID == "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: Revise: %w", ErrMissingID)
	}
	index, err := indexByID(set.Members)
	if err != nil {
		return EmergencyContactSet{}, err
	}
	at, ok := index[contact.ID]
	if !ok {
		return EmergencyContactSet{}, fmt.Errorf("contact: Revise %s: %w", contact.ID, ErrUnknownMember)
	}
	if !validRelationship(contact.Relationship) {
		return EmergencyContactSet{}, fmt.Errorf("contact: Revise: unknown relationship")
	}
	contact.WorkerID = set.WorkerID
	members := append([]EmergencyContact(nil), set.Members...)
	members[at] = contact
	if _, err := indexByID(members); err != nil {
		return EmergencyContactSet{}, err
	}
	set.Members = members
	set.Revision++
	return seal(set), nil
}

// Remove drops one member by ID. Priorities are never renumbered behind
// the caller: gaps stay explicit until REORDER states the full order.
func Remove(set EmergencyContactSet, id string) (EmergencyContactSet, error) {
	if id == "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: Remove: %w", ErrMissingID)
	}
	members := make([]EmergencyContact, 0, len(set.Members))
	found := false
	for _, member := range set.Members {
		if member.ID == id {
			found = true
			continue
		}
		members = append(members, member)
	}
	if !found {
		return EmergencyContactSet{}, fmt.Errorf("contact: Remove %s: %w", id, ErrUnknownMember)
	}
	set.Members = members
	set.Revision++
	return seal(set), nil
}

// Reorder restates every member's priority at once. The order must name
// every member exactly once with unique priorities: partial orders lose
// members and are refused.
func Reorder(set EmergencyContactSet, priorities map[string]int) (EmergencyContactSet, error) {
	if len(priorities) != len(set.Members) {
		return EmergencyContactSet{}, fmt.Errorf("contact: Reorder names %d of %d: %w", len(priorities), len(set.Members), ErrMemberLost)
	}
	members := append([]EmergencyContact(nil), set.Members...)
	for i, member := range members {
		priority, ok := priorities[member.ID]
		if !ok {
			return EmergencyContactSet{}, fmt.Errorf("contact: Reorder drops %s: %w", member.ID, ErrMemberLost)
		}
		members[i].Priority = priority
	}
	if _, err := indexByID(members); err != nil {
		return EmergencyContactSet{}, err
	}
	set.Members = members
	set.Revision++
	return seal(set), nil
}

// Correct revises one member with its correction reason recorded.
func Correct(set EmergencyContactSet, contact EmergencyContact, reason string) (EmergencyContactSet, error) {
	if strings.TrimSpace(reason) == "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: Correct: reason is required")
	}
	return Revise(set, contact)
}

// LinkPerson binds one member's supplied person to its canonical
// reference with linkage evidence. Until then the name is display-only.
func LinkPerson(set EmergencyContactSet, id, personRef, evidenceRef string) (EmergencyContactSet, error) {
	if id == "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: LinkPerson: %w", ErrMissingID)
	}
	if strings.TrimSpace(personRef) == "" || strings.TrimSpace(evidenceRef) == "" {
		return EmergencyContactSet{}, fmt.Errorf("contact: LinkPerson: person and evidence references are required")
	}
	index, err := indexByID(set.Members)
	if err != nil {
		return EmergencyContactSet{}, err
	}
	at, ok := index[id]
	if !ok {
		return EmergencyContactSet{}, fmt.Errorf("contact: LinkPerson %s: %w", id, ErrUnknownMember)
	}
	members := append([]EmergencyContact(nil), set.Members...)
	members[at].PersonRef = personRef
	members[at].PersonLinked = true
	set.Members = members
	set.Revision++
	return seal(set), nil
}

// CanonicalPerson returns the canonical reference or refuses an unlinked
// supplied name.
func CanonicalPerson(set EmergencyContactSet, id string) (string, error) {
	for _, member := range set.Members {
		if member.ID == id {
			if !member.IsCanonical() {
				return "", fmt.Errorf("contact: CanonicalPerson %s: %w", id, ErrUnlinkedPerson)
			}
			return member.PersonRef, nil
		}
	}
	return "", fmt.Errorf("contact: CanonicalPerson %s: %w", id, ErrUnknownMember)
}

// Ordered returns members by ascending priority.
func Ordered(set EmergencyContactSet) []EmergencyContact {
	ordered := append([]EmergencyContact(nil), set.Members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	return ordered
}

// DisclosedContact is the minimum-disclosure view of one member.
type DisclosedContact struct {
	ID           string
	Relationship Relationship
	Priority     int
	PersonLinked bool
	Reachability []string
	Notes        string
}

// Disclose renders the set for one clearance: standard callers see
// membership, order, relationship and linkage; emergency callers
// additionally see reachability and notes. Inaccessible notes never leak.
func Disclose(set EmergencyContactSet, emergency bool) []DisclosedContact {
	ordered := Ordered(set)
	out := make([]DisclosedContact, 0, len(ordered))
	for _, member := range ordered {
		view := DisclosedContact{
			ID: member.ID, Relationship: member.Relationship,
			Priority: member.Priority, PersonLinked: member.PersonLinked,
		}
		if emergency {
			view.Reachability = append([]string(nil), member.Reachability...)
			view.Notes = member.Notes
		}
		out = append(out, view)
	}
	return out
}

// Observation is one external provider snapshot of the set.
type Observation struct {
	Members []EmergencyContact
	// Accepted marks provider acknowledgement: acknowledgement is
	// evidence, never closure.
	Accepted bool
}

// Reconcile compares the local revision against an external observation:
// exact membership, order and values reconcile; anything else opens a
// scoped repair naming the dimension.
func Reconcile(set EmergencyContactSet, observed Observation) (string, error) {
	if len(observed.Members) != len(set.Members) {
		return "repair/membership", nil
	}
	local := Ordered(set)
	theirs := append([]EmergencyContact(nil), observed.Members...)
	sort.Slice(theirs, func(i, j int) bool { return theirs[i].Priority < theirs[j].Priority })
	for i, member := range local {
		other := theirs[i]
		if member.ID != other.ID || member.Priority != other.Priority {
			return "repair/order", nil
		}
		if member.Relationship != other.Relationship ||
			strings.Join(member.Reachability, ",") != strings.Join(other.Reachability, ",") ||
			member.PersonRef != other.PersonRef || member.PersonLinked != other.PersonLinked {
			return "repair/values", nil
		}
	}
	return "reconciled", nil
}

// Directory is the mutex-guarded worker set registry for concurrent
// membership updates.
type Directory struct {
	mu   sync.Mutex
	sets map[string]EmergencyContactSet
}

// NewDirectory returns an empty registry.
func NewDirectory() *Directory {
	return &Directory{sets: make(map[string]EmergencyContactSet)}
}

// Apply runs one set operation atomically per worker.
func (d *Directory) Apply(workerID string, op func(EmergencyContactSet) (EmergencyContactSet, error)) (EmergencyContactSet, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	set, ok := d.sets[workerID]
	if !ok {
		set = NewSet(workerID)
	}
	next, err := op(set)
	if err != nil {
		return EmergencyContactSet{}, err
	}
	d.sets[workerID] = next
	return next, nil
}

// Get returns one worker's set.
func (d *Directory) Get(workerID string) (EmergencyContactSet, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	set, ok := d.sets[workerID]
	return set, ok
}
