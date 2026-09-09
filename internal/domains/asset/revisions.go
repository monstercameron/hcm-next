package asset

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const schemaVersion = 1

// Version reports the asset contract version.
func Version() int { return schemaVersion }

// Canonical returns the immutable receipt encoding. Evidence content is
// represented by its reference and never exposed by Explain.
func (r Receipt) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.asset.Receipt", schemaVersion).
		Value("id", r.ID).Value("issuer", r.Issuer).
		String("issued_at", r.IssuedAt.UTC().Format(time.RFC3339Nano)).
		Bool("verified", r.Verified).String("evidence", r.Evidence).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (i InventoryRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.asset.InventoryRevision", schemaVersion).
		Value("inventory_id", i.InventoryID).Value("owner", i.Owner).
		String("classification", i.Classification).String("serial_number", i.SerialNumber).
		Value("revision", i.Revision).String("effective_at", i.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String("status", string(i.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical returns the digestable inventory revision encoding.
func (i InventoryRevision) Canonical() []byte {
	if err := i.Validate(); err != nil {
		return nil
	}
	return i.body()
}

// Digest returns the canonical digest of the inventory revision.
func (i InventoryRevision) Digest() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(i.body()), nil
}

func (c CustodyRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.asset.CustodyRevision", schemaVersion).
		Value("asset", c.Asset).Value("worker", optionalAssetRef{ref: c.Worker}).
		String("location", c.Location).String("condition", c.Condition).
		Value("assignee_receipt", optionalReceipt{receipt: c.AssigneeReceipt}).
		Value("issuer_receipt", optionalReceipt{receipt: c.IssuerReceipt}).
		Value("revision", c.Revision).String("effective_at", c.EffectiveAt.UTC().Format(time.RFC3339Nano)).
		String("status", string(c.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Canonical returns the digestable append-only custody event encoding.
func (c CustodyRevision) Canonical() []byte {
	if err := c.Validate(); err != nil {
		return nil
	}
	return c.body()
}

// Digest returns the canonical digest of the custody event.
func (c CustodyRevision) Digest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	return canonicalbytes.Digest(c.body()), nil
}

type optionalAssetRef struct {
	ref interface {
		Canonical() []byte
		Validate() error
	}
}

func (r optionalAssetRef) Canonical() []byte {
	if r.ref == nil || r.ref.Validate() != nil {
		return []byte{0}
	}
	return append([]byte{1}, r.ref.Canonical()...)
}

type optionalReceipt struct{ receipt Receipt }

func (r optionalReceipt) Canonical() []byte {
	if r.receipt.ID.Validate() != nil || r.receipt.Issuer.Validate() != nil {
		return []byte{0}
	}
	return append([]byte{1}, r.receipt.Canonical()...)
}

// InventoryExplanation is structural and deliberately omits serial numbers.
type InventoryExplanation struct {
	Status         Status
	Classification string
	Revision       string
	Digest         string
}

func (i InventoryRevision) Explain() (InventoryExplanation, error) {
	if err := i.Validate(); err != nil {
		return InventoryExplanation{}, err
	}
	return InventoryExplanation{Status: i.Status, Classification: i.Classification, Revision: i.Revision.String(), Digest: canonicalbytes.Digest(i.body())}, nil
}

// CustodyExplanation reports lifecycle and evidence presence without repeating
// worker, location, condition, receipt, or evidence values.
type CustodyExplanation struct {
	Status           Status
	Revision         string
	AssigneeVerified bool
	IssuerVerified   bool
	HasCondition     bool
	HasLocation      bool
	Digest           string
}

func (c CustodyRevision) Explain() (CustodyExplanation, error) {
	if err := c.Validate(); err != nil {
		return CustodyExplanation{}, err
	}
	return CustodyExplanation{
		Status: c.Status, Revision: c.Revision.String(),
		AssigneeVerified: c.AssigneeReceipt.Verified, IssuerVerified: c.IssuerReceipt.Verified,
		HasCondition: c.Condition != "", HasLocation: c.Location != "", Digest: canonicalbytes.Digest(c.body()),
	}, nil
}

// AssetExplanation summarizes both immutable streams without disclosing
// serial numbers, locations, conditions, or receipt evidence.
type AssetExplanation struct {
	InventoryStatus Status
	CustodyEvents   int
	CurrentStatus   Status
	Digest          string
}

// Explain returns a stable structural summary for an inventory and its
// append-only custody history.
func Explain(inventory InventoryRevision, history []CustodyRevision) (AssetExplanation, error) {
	if err := inventory.Validate(); err != nil {
		return AssetExplanation{}, err
	}
	for index, event := range history {
		if err := event.Validate(); err != nil {
			return AssetExplanation{}, err
		}
		if event.Asset != inventory.InventoryID {
			return AssetExplanation{}, fmt.Errorf("%w: custody event is for another asset", ErrInvalidAsset)
		}
		if index > 0 {
			prior := history[index-1]
			order, compareErr := prior.Revision.CompareInStream(event.Revision)
			if !prior.EffectiveAt.Before(event.EffectiveAt) || compareErr != nil || order >= 0 {
				return AssetExplanation{}, ErrChronology
			}
		}
	}
	current := inventory.Status
	if len(history) > 0 {
		current = history[len(history)-1].Status
	}
	w := canonicalbytes.New("hcmnext.domains.asset.AssetExplanation", schemaVersion).
		String("inventory_status", string(inventory.Status)).Int("custody_events", int64(len(history))).
		String("current_status", string(current)).String("inventory_digest", canonicalbytes.Digest(inventory.body()))
	for _, event := range history {
		w.String("custody_digest", canonicalbytes.Digest(event.body()))
	}
	digest, err := w.Digest()
	if err != nil {
		return AssetExplanation{}, err
	}
	return AssetExplanation{InventoryStatus: inventory.Status, CustodyEvents: len(history), CurrentStatus: current, Digest: digest}, nil
}
