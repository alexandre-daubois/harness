// Package modelproxy keeps the model API key on the host. A sandboxed agent gets a token that is only
// good for this proxy, which swaps it for the real key on a fixed list of routes and forwards the request.
// Adapted from scrutineer's internal/worker/model_proxy.go.
package modelproxy
