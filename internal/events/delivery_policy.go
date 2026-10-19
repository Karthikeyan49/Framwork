package events

import "time"

// SQL owns delivery timing. These expected contract values make readiness deny
// missing/stale deployments; callers cannot override them with event metadata.
const (
	DeliveryPolicyVersion = "event-outage-v3"
	PublishLease          = 5 * time.Second
	ReceiptWindow         = 15 * time.Minute
	MaxDeliveryAttempts   = 20
	RetryBase             = 500 * time.Millisecond
	RetryCap              = time.Minute
)
