// Package bufprotovalidatekit is an offline qualification seam for LIB-019.
//
// It deliberately exposes only owned structural violations over protobuf
// reflection. Buf remains a developer-only schema linter, and no Buf or
// Protovalidate type crosses this package's validation boundary.
package bufprotovalidatekit
