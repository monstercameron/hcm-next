package otelmw

import (
	"context"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

type ctxKey int

const traceKey ctxKey = 1

func PropagationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := telemetry.ExtractPropagation(r.Header.Get("traceparent"), r.Header.Get("baggage"))
		if res.SecuritySignal != "" {
			w.Header().Set("X-Trace-Signal", res.SecuritySignal)
		}
		ctx := context.WithValue(r.Context(), traceKey, res.Trace)
		r = r.WithContext(ctx)
		r.Header.Set("traceparent", telemetry.InjectTraceParent(res.Trace))
		r.Header.Set("baggage", telemetry.InjectBaggage(res.Baggage))
		next.ServeHTTP(w, r)
	})
}

func TraceFromContext(ctx context.Context) (telemetry.TraceContext, bool) {
	v := ctx.Value(traceKey)
	tc, ok := v.(telemetry.TraceContext)
	return tc, ok
}
