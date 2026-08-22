package bad

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// every line below is engineered to trigger exactly one rule.

var globalCounter int // expect: noglobals

type Color int // enum

const (
	Red Color = iota
	Green
	Blue
)

type Shape interface {
	area() float64 // unexported method -> sealed
}

type Circle struct{}

func (c Circle) area() float64 { return 0 }

type Square struct{}

func (s Square) area() float64 { return 0 }

func mayFail() error { return errors.New("nope") }

func split() (int, error) { return 0, errors.New("nope") }

func ignoresError() {
	mayFail() // expect: errcheck (dropped error)
}

func ignoresErrorWithBlank() {
	_ = mayFail() // expect: errcheck (blank without annotation)
}

func ignoresErrorWithAnnotation() {
	_ = mayFail() // safe-ignore: this fire-and-forget is deliberate
}

func ignoresErrInTuple() {
	x, _ := split() // expect: errcheck on the blank err
	_ = x
}

func shadowExample() {
	err := mayFail()
	if err != nil {
		err := mayFail() // expect: shadow
		_ = err          // safe-ignore: just to silence errcheck here
	}
	_ = err
}

func badAssert(v any) string { // expect: banany on the parameter
	return v.(string) // expect: forcetypeassert
}

func goodAssert(v any) string { // any-ok: this is a deliberate boundary func
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func swapBug(userID, orderID string) {} // declared with two strings

func callsSwap() {
	swapBug("u-1", "o-2") // expect: namedargs (two strings adjacent, no comments)
}

func callsSwapOK() {
	swapBug( /* userID */ "u-1" /* orderID */, "o-2")
}

func nonExhaustive(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	} // expect: exhaustive (missing Blue)
	return ""
}

func nonExhaustiveType(s Shape) string {
	switch s.(type) {
	case Circle:
		return "circle"
	} // expect: exhaustive (missing Square)
	return ""
}

func fetchResp() (*http.Response, error) { return nil, nil }

func leakedBody() {
	resp, err := fetchResp()
	if err != nil {
		return
	}
	_ = resp // expect: bodyclose
}

func leakedBodyIgnored() {
	resp, err := fetchResp() // safe-ignore: probe — body discarded by transport
	if err != nil {
		return
	}
	_ = resp
}

func notLeaked() {
	resp, err := fetchResp()
	if err != nil {
		return
	}
	defer resp.Body.Close() // safe-ignore: cleanup
}

type closer struct{}

func (closer) Close() error { return nil }

func deferredClose() {
	var c closer
	defer c.Close() // expect: errcheck (defer Close is flagged, not allowlisted)
}

func goDropsError() {
	go mayFail() // goroutine-ok: testing errcheck on go; expect: errcheck
}

func discardedOK(v any) string { // any-ok: boundary for the discarded-ok assert
	s, _ := v.(string) // expect: forcetypeassert (discarded ok)
	return s
}

func noClientTimeout() {
	var c http.Client // expect: httptimeout
	_ = c
	_ = new(http.Client) // expect: httptimeout
}

func noServerTimeout() {
	_ = &http.Server{} // expect: httptimeout
}

func defaultClientWithContext(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://x", nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req) // ok: request carries the deadline
	if err != nil {
		return
	}
	defer resp.Body.Close() // safe-ignore: cleanup
	_ = resp
}

func wrongContext(ctx context.Context) {
	fmt.Println(ctx)
	_ = context.Background() // expect: contextcheck
}

func ignoredBackground(ctx context.Context) {
	_ = ctx
	_ = context.Background() // safe-ignore: detached cleanup must outlive request
}

func spawnsBareGoroutine() {
	go fmt.Println("hi") // expect: goroutine (no lifecycle primitive)
}

func spawnsOKGoroutine() {
	go fmt.Println("hi") // goroutine-ok: deliberate fire-and-forget
}

type customErr struct{}

func (customErr) Error() string { return "x" }

func errCompareSentinel(err error) bool {
	return err == io.EOF // expect: errorlint (use errors.Is)
}

func errCompareSentinelOK(err error) bool {
	return err == io.EOF // safe-ignore: io.EOF documented never to be wrapped
}

func errCompareNilOK(err error) bool {
	return err == nil // not an issue (nil literal exempted)
}

func errAssertBad(err error) string {
	e := err.(customErr) // expect: errorlint (use errors.As)
	return e.Error()
}

func errorfBadVerb(err error) error {
	return fmt.Errorf("oh no: %v", err) // expect: errorlint (use %w)
}

func errorfGood(err error) error {
	return fmt.Errorf("oh no: %w", err) // ok
}
