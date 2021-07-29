// Package event is event handler for scrape result
package event

// Event scrape result data
type Event struct {
	Namespace string
	Type      string

	Resource interface{}
}

