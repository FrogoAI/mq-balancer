package subscriber

import "errors"

var (
	// ErrConnectionClosed marks a subscription that can no longer be read from.
	// The reader treats it as recoverable and re-establishes the subscription.
	ErrConnectionClosed = errors.New("connection closed")

	// ErrDraining marks a subscription that is draining after Stop. Terminal,
	// but a clean shutdown rather than a failure.
	ErrDraining = errors.New("connection draining")

	// ErrTimeout marks an idle poll: no message arrived within the read
	// timeout. This is the normal quiet path, not a failure.
	ErrTimeout = errors.New("read timeout")

	// ErrSlowConsumer marks messages dropped because the client buffer
	// overflowed. The subscription stays valid and readable.
	ErrSlowConsumer = errors.New("slow consumer")

	// ErrMaxMessages marks a subscription that auto-unsubscribed after
	// reaching its message limit. Terminal and legitimate.
	ErrMaxMessages = errors.New("max messages reached")

	// ErrSubscriptionFailed is returned by Process when the reader could not be
	// kept alive and no retry can recover it.
	ErrSubscriptionFailed = errors.New("subscription failed")
)
