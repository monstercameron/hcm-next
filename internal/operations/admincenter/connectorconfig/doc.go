// Package connectorconfig implements the ADMIN-004 connector/configuration
// operations-center contract. It is a projection and safety-policy boundary:
// it never executes a connector, reruns a parent workflow, or persists a
// configuration change.
package connectorconfig
