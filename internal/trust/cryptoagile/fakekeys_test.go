package cryptoagile

import "testing"

func TestFakeKeySource_Ed25519(t *testing.T) {
	f := NewFakeKeySource()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	if err := f.AddEd25519("s1", seed); err != nil {
		t.Fatalf("AddEd25519: %v", err)
	}
	sig, err := f.Sign("s1", []byte("payload"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ok, err := f.Verify("s1", []byte("payload"), sig)
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v, want true, nil", ok, err)
	}
	ok, err = f.Verify("s1", []byte("other"), sig)
	if err != nil || ok {
		t.Fatalf("Verify(wrong message) = %v, %v, want false, nil", ok, err)
	}
}

func TestFakeKeySource_HMAC(t *testing.T) {
	f := NewFakeKeySource()
	if err := f.AddHMAC("h1", []byte("a shared secret key")); err != nil {
		t.Fatalf("AddHMAC: %v", err)
	}
	sig, err := f.Sign("h1", []byte("payload"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ok, err := f.Verify("h1", []byte("payload"), sig)
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v, want true, nil", ok, err)
	}
	if ok, _ := f.Verify("h1", []byte("payload"), append([]byte{}, sig[:len(sig)-1]...)); ok {
		t.Fatalf("Verify(truncated sig) = true, want false")
	}
}

func TestFakeKeySource_UnknownSuite(t *testing.T) {
	f := NewFakeKeySource()
	if _, err := f.Sign("ghost", []byte("m")); err == nil {
		t.Fatalf("Sign(ghost) = nil error, want error")
	}
	if _, err := f.Verify("ghost", []byte("m"), []byte{1}); err == nil {
		t.Fatalf("Verify(ghost) = nil error, want error")
	}
}

func TestFakeKeySource_RejectsBadKeyMaterial(t *testing.T) {
	f := NewFakeKeySource()
	if err := f.AddEd25519("s1", []byte("too short")); err == nil {
		t.Fatalf("AddEd25519(short seed) = nil, want error")
	}
	if err := f.AddHMAC("h1", nil); err == nil {
		t.Fatalf("AddHMAC(empty key) = nil, want error")
	}
}
