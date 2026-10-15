//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestM8ProducerEnvelopeConsumerAuditTraceCorrelation(t *testing.T) {
	m8Database(t)
	previous := otel.GetTracerProvider()
	previousPropagation := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	otel.SetTracerProvider(provider)
	defer func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previous)
		otel.SetTextMapPropagator(previousPropagation)
	}()
	f, h, base, token, _ := m5Fixture(t)
	traceID := "00112233445566778899aabbccddeeff"
	raw, _ := json.Marshal(data.Command{Operation: "create", Values: m5Values("trace fixture")})
	request := httptest.NewRequest("POST", base+"/commands", strings.NewReader(string(raw)))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("traceparent", "00-"+traceID+"-0011223344556677-01")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	d, w := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	pointer := m8Claim(t, dispatch)
	var body []byte
	if e := f.operator.QueryRow(`SELECT body FROM runtime.event_envelopes WHERE tenant_id=$1 AND id=$2`, f.tenantA, pointer.EventID).Scan(&body); e != nil {
		t.Fatal(e)
	}
	envelope, e := events.DecodeEnvelope(body, pointer)
	if e != nil || !strings.HasPrefix(envelope.TraceParent, "00-"+traceID+"-") || envelope.RequestID != response.Header().Get("X-Request-ID") {
		t.Fatal("producer correlation lost")
	}
	if e = dispatch.Finish(context.Background(), pointer, true); e != nil {
		t.Fatal(e)
	}
	if e = m8Consumer(w).Consume(context.Background(), pointer); e != nil {
		t.Fatal(e)
	}
	var auditedTrace, requestID, eventID, consumerID string
	var sequence int64
	if e = f.operator.QueryRow(`SELECT trace_id,request_id,state->>'event_id',state->>'consumer_id',(state->>'aggregate_sequence')::bigint FROM audit.entries WHERE tenant_id=$1 AND action='events.consume'`, f.tenantA).Scan(&auditedTrace, &requestID, &eventID, &consumerID, &sequence); e != nil || auditedTrace != traceID || requestID != envelope.RequestID || eventID != pointer.EventID || consumerID != events.ConsumerID || sequence != 1 {
		t.Fatal("consumer correlation lost", e)
	}
}
