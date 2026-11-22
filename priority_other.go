//go:build !unix

package main

// lowerPriority does nothing where there is no niceness to set.
func lowerPriority(int) {}
