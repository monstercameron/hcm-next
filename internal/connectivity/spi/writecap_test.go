package spi_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/spi"
)

func TestDeclaredAmendmentsIsEmptyInThisRelease(t *testing.T) {
	if got := spi.DeclaredAmendments(); len(got) != 0 {
		t.Fatalf("DeclaredAmendments = %v, want none in P1A", got)
	}
}

func TestDeclareWriteCapabilityRefusesInvalidDeclaration(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	dec, err := spi.DeclareWriteCapability(spi.WriteCapabilityDeclaration{}, now)
	if !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	if !dec.Refused || dec.Code != spi.RefusalInvalidDeclaration || dec.DecidedAt != now {
		t.Fatalf("decision=%+v", dec)
	}
}

func TestDeclareWriteCapabilityRefusesNoAmendment(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	decl := spi.WriteCapabilityDeclaration{Object: connectivity.ObjectWorker, RequestedBy: "test"}
	dec, err := spi.DeclareWriteCapability(decl, now)
	if !errors.Is(err, connectivity.ErrPermission) {
		t.Fatalf("got %v, want ErrPermission", err)
	}
	if !dec.Refused || dec.Code != spi.RefusalNoAmendment || dec.Object != connectivity.ObjectWorker {
		t.Fatalf("decision=%+v", dec)
	}
}

func TestDeclareWriteCapabilityRefusesUnknownAmendment(t *testing.T) {
	now := time.Unix(300, 0).UTC()
	decl := spi.WriteCapabilityDeclaration{
		Object:                   connectivity.ObjectWorker,
		RequestedBy:              "test",
		AuthorityAmendmentDigest: "no-such-amendment",
	}
	dec, err := spi.DeclareWriteCapability(decl, now)
	if !errors.Is(err, connectivity.ErrPermission) {
		t.Fatalf("got %v, want ErrPermission", err)
	}
	if !dec.Refused || dec.Code != spi.RefusalUnknownAmendment {
		t.Fatalf("decision=%+v", dec)
	}
}

func TestDeclareWriteCapabilityIsPureInNow(t *testing.T) {
	decl := spi.WriteCapabilityDeclaration{Object: connectivity.ObjectWorker, RequestedBy: "test"}
	now := time.Unix(400, 0).UTC()
	d1, _ := spi.DeclareWriteCapability(decl, now)
	d2, _ := spi.DeclareWriteCapability(decl, now)
	if d1 != d2 {
		t.Fatalf("DeclareWriteCapability is not a pure function of its inputs: %+v vs %+v", d1, d2)
	}
}

func TestWriteCapabilityDeclarationValidate(t *testing.T) {
	badObject := spi.WriteCapabilityDeclaration{Object: "NOT_A_KIND", RequestedBy: "test"}
	if err := badObject.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	noRequester := spi.WriteCapabilityDeclaration{Object: connectivity.ObjectWorker}
	if err := noRequester.Validate(); !errors.Is(err, connectivity.ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
	ok := spi.WriteCapabilityDeclaration{Object: connectivity.ObjectWorker, RequestedBy: "test"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestRefusalCodeValid(t *testing.T) {
	for _, c := range []spi.RefusalCode{spi.RefusalInvalidDeclaration, spi.RefusalNoAmendment, spi.RefusalUnknownAmendment} {
		if !c.Valid() {
			t.Fatalf("%v should be valid", c)
		}
	}
	if spi.RefusalCode("SOMETHING_ELSE").Valid() {
		t.Fatal("unknown refusal code must not be valid")
	}
}
