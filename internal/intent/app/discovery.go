package app

import (
	"encoding/json"
	"net/http"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/monstercameron/hcm-next/internal/transport"
	"github.com/monstercameron/hcm-next/internal/transport/envelope"
	"github.com/monstercameron/hcm-next/internal/transport/manifest"
)

// DiscoveryPath is where the HTTP edge serves the API-001 discovery document.
//
// It is served on the HTTP edge only. The RegistryService contract publishes
// definitions and capabilities but declares no discovery method, and adding an
// unpublished RPC would be a wire contract this build invented for itself.
const DiscoveryPath = "/v1/discovery"

// discoveryHandler serves the rendered discovery document to an authenticated
// caller.
//
// Authentication is required, and it is the same admission the RPC surfaces
// run: the endpoint contract's own non-goal is that discovery "never reveals
// tenant-specific unavailable capabilities to an unauthorized caller", and an
// anonymous discovery endpoint would publish this cell's whole served shape -
// every endpoint, every capability, every refusal reason - to anyone who can
// reach the port.
//
// The document itself is tenant-independent: it is rendered from the compiled
// manifest, the compiled definition catalog and this cell's published
// capability table, none of which vary by caller. So the response is the same
// for every authenticated principal, and there is nothing to filter.
type discoveryHandler struct {
	config   transport.Config
	document []byte
}

// newDiscoveryHandler renders the document once, at composition, and serves
// the bytes.
//
// Rendering is a pure function of the build, so doing it per request would
// spend work to produce the same answer. Doing it at composition also means a
// manifest this build cannot render fails the cell's construction rather than
// the first caller's request.
func newDiscoveryHandler(cfg transport.Config, doc *manifest.DiscoveryDocument) (*discoveryHandler, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return &discoveryHandler{config: cfg, document: body}, nil
}

// ServeHTTP implements [http.Handler].
func (h *discoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDiscoveryError(w, envelope.New(envelope.CodeInvalidArgument,
			"discovery.method_not_allowed",
			"the discovery document is read with GET"))
		return
	}
	if _, _, ownedErr := transport.PreAdmit(r.Context(), h.config,
		transport.MapMetadata(r.Header), DiscoveryPath); ownedErr != nil {
		writeDiscoveryError(w, ownedErr)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.document)
}

// writeDiscoveryError renders one owned error as the canonical typed detail,
// under the status the projection table assigns its code. It is the same
// payload the RPC surfaces carry, so a client parses one error shape whichever
// path it took.
func writeDiscoveryError(w http.ResponseWriter, ownedErr *envelope.Error) {
	body, err := protojson.Marshal(ownedErr.Detail())
	if err != nil {
		http.Error(w, "{}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ownedErr.HTTPStatus())
	_, _ = w.Write(body)
}
