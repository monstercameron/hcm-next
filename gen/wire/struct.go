package wire

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// MarshalStruct encodes fields as a protobuf Struct. It is the temporary wire
// adapter for contracts that have not yet acquired a generated message.
func MarshalStruct(fields map[string]any) ([]byte, error) {
	s, err := structpb.NewStruct(fields)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(s)
}
