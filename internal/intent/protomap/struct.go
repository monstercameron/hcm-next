package protomap

import (
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Struct is the temporary dynamic request shape used while P1A's per-domain
// request messages are still authored. It is exposed from the one approved
// Protobuf-to-intent seam so application code never needs to import the
// Protobuf runtime directly.
//
// It is an alias deliberately: the replacement with generated request
// messages is local to this seam, and callers retain the structural decoding
// policy rather than acquiring a second wire implementation.
type Struct = structpb.Struct

// NumberValue and StringValue keep the temporary Struct field inspection in
// the approved wire seam. They are only used by unexported application
// decoding helpers and are not capability or domain-port types.
type NumberValue = structpb.Value_NumberValue
type StringValue = structpb.Value_StringValue

// DecodeStruct decodes the temporary google.protobuf.Struct request payload.
// P1A's application resolver consumes the returned value immediately and
// maps it into owned domain inputs; neither a Protobuf type nor a dynamic
// value crosses an exported capability/domain port.
func DecodeStruct(wire []byte) (*Struct, error) {
	var s structpb.Struct
	if err := proto.Unmarshal(wire, &s); err != nil {
		return nil, fmt.Errorf("protomap: request payload is not a google.protobuf.Struct: %w", err)
	}
	return &s, nil
}

// MarshalDeterministic serializes an already-projected generated message for
// persistence. Keeping the runtime call here ensures application services
// depend on the owned mapping seam rather than on protobuf mechanics.
func MarshalDeterministic(message proto.Message) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(message)
}

// Unmarshal decodes a generated message at the approved mapping seam.
func Unmarshal(wire []byte, message proto.Message) error { return proto.Unmarshal(wire, message) }
