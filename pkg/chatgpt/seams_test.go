package chatgpt

import "context"

// Test helpers swapping the package seams (issuer URL, callback bind address,
// browser opener). Tests using them must not run in parallel.

func setAuthBaseURLForTests(url string) (restore func()) {
	old := authBaseURL
	authBaseURL = url
	return func() { authBaseURL = old }
}

func setCallbackAddrForTests(addr string) (restore func()) {
	old := callbackAddr
	callbackAddr = addr
	return func() { callbackAddr = old }
}

func setBrowserOpenerForTests(open func(ctx context.Context, url string) error) (restore func()) {
	old := openBrowser
	openBrowser = open
	return func() { openBrowser = old }
}
