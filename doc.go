// Package glassnode provides a context-aware client for the Glassnode HTTP API.
//
// Create one Client and reuse it across goroutines. Authentication and HTTP
// behavior are configured explicitly; the package never reads environment
// variables or files. Timestamps in responses are Unix seconds.
package glassnode
