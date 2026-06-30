package main

import "testing"

func TestIsErrorPage_StrongPatterns(t *testing.T) {
	// Strong patterns flag an error page regardless of length.
	long := "Just a moment... " + repeat("filler ", 500)
	cases := []string{
		"DNS_PROBE_FINISHED_NXDOMAIN",
		"This site can't be reached",
		"Checking your browser before accessing",
		long,
	}
	for _, md := range cases {
		if !isErrorPage(md) {
			t.Errorf("isErrorPage(%.40q) = false, want true", md)
		}
	}
}

func TestIsErrorPage_WeakPatternsOnlyWhenShort(t *testing.T) {
	// A short page containing a weak pattern is an error page.
	short := "404 Not Found"
	if !isErrorPage(short) {
		t.Errorf("short weak page: isErrorPage(%q) = false, want true", short)
	}

	// A long article that merely discusses the same phrase is NOT an error page.
	article := "How to handle 404 Not Found errors in your REST API. " +
		repeat("This guide covers status codes, retries, and rate limit handling. ", 50)
	if isErrorPage(article) {
		t.Errorf("long technical article wrongly flagged as error page")
	}
}

func TestIsErrorPage_Empty(t *testing.T) {
	if isErrorPage("") {
		t.Error("isErrorPage(\"\") = true, want false")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
