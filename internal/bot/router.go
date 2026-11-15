// Package bot routes messenger commands and actions to feature handlers.
//
// The router is defined here so features can depend on it; its command, action
// and fallback handling is implemented in M2.
package bot

// Router dispatches resolved messenger events to the handlers registered by
// features.
type Router struct{}

// NewRouter returns an empty router.
func NewRouter() *Router { return &Router{} }
