package events

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"io"
	"regexp"
	"time"
)

const ConsumerID = "preview.receipt.v1"

var ErrDelivery = errors.New("event delivery failed")
var ErrUnroutable = errors.New("event pointer is unroutable")
var ErrNotReady = errors.New("event publication is unconfirmed or paused")
var ErrDead = errors.New("event has durable dead-letter evidence")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validUUID(s string) bool { return uuidPattern.MatchString(s) }
func validHash(s string) bool { return hashPattern.MatchString(s) }
func validEventType(s string) bool {
	return s == "record.created" || s == "record.updated" || s == "record.deleted"
}
func validTrace(s string) bool {
	if s == "" {
		return true
	}
	ctx := (propagation.TraceContext{}).Extract(context.Background(), propagation.MapCarrier{"traceparent": s})
	sc := trace.SpanContextFromContext(ctx)
	return len(s) == 55 && s[:3] == "00-" && sc.IsValid()
}

// Pointer is a routing reference, not an authorization context or a payload.
type Pointer struct {
	SchemaVersion string `json:"schema_version"`
	TenantID      string `json:"tenant_id"`
	EventID       string `json:"event_id"`
	BodyHash      string `json:"body_hash"`
	ConsumerID    string `json:"consumer_id"`
	Generation    int64  `json:"generation"`
	LeaseToken    string `json:"lease_token"`
}

func (p Pointer) Valid() bool {
	return p.SchemaVersion == "1.0.0" && validUUID(p.TenantID) && validUUID(p.EventID) && validHash(p.BodyHash) && p.ConsumerID == ConsumerID && p.Generation > 0 && validUUID(p.LeaseToken)
}
func (p Pointer) Bytes() ([]byte, error) {
	if !p.Valid() {
		return nil, ErrUnroutable
	}
	return json.Marshal(p)
}
func DecodePointer(raw []byte) (Pointer, error) {
	var p Pointer
	if len(raw) > 1024 || decode(raw, &p) != nil || !p.Valid() {
		return p, ErrUnroutable
	}
	canonical, _ := json.Marshal(p)
	if !bytes.Equal(raw, canonical) {
		return Pointer{}, ErrUnroutable
	}
	return p, nil
}

type Payload struct {
	RecordID      string `json:"record_id"`
	RecordVersion int64  `json:"record_version"`
}
type Envelope struct {
	SchemaVersion     string    `json:"schema_version"`
	Epoch             string    `json:"epoch"`
	Mode              string    `json:"mode"`
	Topic             string    `json:"topic"`
	ID                string    `json:"id"`
	TenantID          string    `json:"tenant_id"`
	ActorID           string    `json:"actor_id"`
	AppID             string    `json:"app_id"`
	MetadataRevision  int64     `json:"metadata_revision"`
	IRHash            string    `json:"ir_hash"`
	AggregateType     string    `json:"aggregate_type"`
	AggregateID       string    `json:"aggregate_id"`
	AggregateSequence int64     `json:"aggregate_sequence"`
	EventType         string    `json:"event_type"`
	OccurredAt        time.Time `json:"occurred_at"`
	RequestID         string    `json:"request_id"`
	TraceParent       string    `json:"traceparent"`
	Payload           Payload   `json:"payload"`
}

func DecodeEnvelope(raw []byte, p Pointer) (Envelope, error) {
	var e Envelope
	sum := sha256.Sum256(raw)
	if !p.Valid() || len(raw) > 8192 || hex.EncodeToString(sum[:]) != p.BodyHash || decode(raw, &e) != nil || e.SchemaVersion != "1.0.0" || e.Epoch != "m8-v1" || e.Mode != "preview" || e.Topic != "record.changes.v1" || e.ID != p.EventID || e.TenantID != p.TenantID || !validUUID(e.ActorID) || e.AppID == "" || len(e.AppID) > 160 || e.MetadataRevision < 1 || !validHash(e.IRHash) || e.AggregateType == "" || len(e.AggregateType) > 160 || !validUUID(e.AggregateID) || e.AggregateSequence < 1 || !validEventType(e.EventType) || e.OccurredAt.IsZero() || len(e.RequestID) > 64 || !validTrace(e.TraceParent) || e.Payload.RecordID != e.AggregateID || e.Payload.RecordVersion < 1 {
		return Envelope{}, ErrDelivery
	}
	return e, nil
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrDelivery
	}
	return nil
}
