// Package event is event handler for scrape result
package event

// Event scrape result data
type Event struct {
	Namespace string
	Type      string

	Resource interface{}

	// OldResource is the previous state of Resource on an update, and nil when
	// the resource is seen for the first time. Handlers use it to notify only
	// on a state transition, because the informer also redelivers unchanged
	// resources as updates (e.g. on a relist after a watch failure).
	OldResource interface{}
}
