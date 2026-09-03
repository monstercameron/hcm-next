// Package transport contains bounded, provider-neutral connectivity transports.
//
// Protocol façades deliberately expose requests and responses, never sockets or
// vendor SDK values. All outbound operations pass through the same bounds,
// destination trust, deadline, and error normalization checks.
package transport
