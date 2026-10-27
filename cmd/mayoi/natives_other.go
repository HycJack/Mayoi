//go:build !windows || !amd64

package main

// unpackNative is nothing here: no library is built in for these
// platforms, whose terminals find theirs where MyGo puts them.
func unpackNative() {}
