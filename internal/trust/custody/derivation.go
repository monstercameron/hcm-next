package custody

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

// DerivedValue is a scoped provider result. It is suitable for a domain PRF
// input but is never the underlying custody key.
type DerivedValue struct {
	Handle    Handle `json:"handle"`
	Algorithm string `json:"algorithm"`
	Output    []byte `json:"output"`
}

// KeyDeriver lets a domain request a scoped derivation without retrieving key
// material. Implementations perform the derivation inside provider custody.
type KeyDeriver interface {
	Derive(Context, Handle, []byte) (DerivedValue, Receipt, error)
}

type Deriver = KeyDeriver

func (f *InMemoryFake) Derive(ctx Context, handle Handle, label []byte) (DerivedValue, Receipt, error) {
	if err := f.validate(ctx, handle); err != nil {
		return DerivedValue{}, Receipt{}, err
	}
	if handle.Kind != Key {
		return DerivedValue{}, Receipt{}, fmt.Errorf("%w: derivation requires a key", ErrWrongObjectKind)
	}
	f.mu.Lock()
	lifecycle, ok := f.objects[handle]
	f.mu.Unlock()
	if !ok {
		return DerivedValue{}, Receipt{}, fmt.Errorf("%w: %s", ErrObjectNotFound, handle.ID)
	}
	if lifecycle.Status == StatusRevoked {
		return DerivedValue{}, Receipt{}, ErrObjectRevoked
	}
	// This deterministic seed belongs to the fake provider. It models a key
	// that remains inside custody; callers receive only a scoped PRF result.
	seed := sha256.Sum256([]byte("in-memory-custody-key\x00" + handle.ID + "\x00" + handle.Version + "\x00" + handle.Tenant + "\x00" + handle.Region))
	mac := hmac.New(sha256.New, seed[:])
	_, _ = mac.Write(label)
	output := mac.Sum(nil)
	return DerivedValue{Handle: handle, Algorithm: "HMAC-SHA256", Output: output}, f.receipt(ctx, handle, Sign, f.clock().UTC()), nil
}

var _ KeyDeriver = (*InMemoryFake)(nil)
