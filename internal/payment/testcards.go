package payment

// Test card numbers, in the style of Stripe's test cards. The stubs fail on
// these so every outcome can be reached over HTTP. Any other value succeeds.
const (
	// TestCardDecline makes Authorize return ErrDeclined: the order is rejected.
	TestCardDecline = "9999"
	// TestCardFulfillmentFails makes fulfillment fail and the void succeed:
	// the order is cancelled.
	TestCardFulfillmentFails = "9998"
	// TestCardVoidFails makes fulfillment and the void both fail: the order
	// needs attention.
	TestCardVoidFails = "9997"
)
