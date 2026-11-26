package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// ffmpeg writes the camera address into its error messages, credentials
// included, in the escaping of the URL's userinfo ("geheim%20wort%21"). The
// log of Prusa Connect Local must not get them in any spelling, so a line is
// redacted twice before it is written:
//
//  1. every "scheme://userinfo@" becomes "scheme://xxxxx@", whatever the
//     escaping, and
//  2. every known spelling of the password (plain, path, query and userinfo
//     escaping) becomes "xxxxx", for a message that quotes it on its own.
//
// Lines are redacted whole: a password split across two writes is held back
// until its line is complete.

// userinfoAt finds the userinfo of an address up to its last "@" before the
// path. A password with an unescaped "@" is covered in full.
var userinfoAt = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s]*@`)

// redact removes the credentials of an address from one line.
func redact(line string, secrets []string) string {
	line = userinfoAt.ReplaceAllString(line, "${1}xxxxx@")
	for _, secret := range secrets {
		line = strings.ReplaceAll(line, secret, "xxxxx")
	}

	return line
}

// secretsOf answers every spelling of the address's password and userinfo
// that may appear in a message, longest first.
func secretsOf(address string) []string {
	parsed, err := url.Parse(address)
	if err != nil || parsed.User == nil {
		return nil
	}

	password, set := parsed.User.Password()
	if !set || password == "" {
		return nil
	}

	forms := []string{
		parsed.User.String(),
		strings.TrimPrefix(url.UserPassword("", password).String(), ":"),
		url.PathEscape(password),
		url.QueryEscape(password),
		password,
	}

	out := make([]string, 0, len(forms))
	for _, form := range forms {
		if form != "" && !slices.Contains(out, form) {
			out = append(out, form)
		}
	}

	return out
}

// maxHeldLine bounds a line that never ends; it is redacted and written once
// it is this long.
const maxHeldLine = 64 << 10

// redactedLog is ffmpeg's stderr: it writes whole, redacted lines with a
// prefix to the module's own stderr, which the host logs.
type redactedLog struct {
	mu      sync.Mutex
	out     io.Writer
	prefix  string
	secrets []string
	held    strings.Builder
}

func newRedactedLog(prefix, address string) *redactedLog {
	return &redactedLog{out: os.Stderr, prefix: prefix, secrets: secretsOf(address)}
}

func (l *redactedLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.held.Write(b)
	text := l.held.String()

	last := strings.LastIndexByte(text, '\n')
	if last < 0 && len(text) < maxHeldLine {
		return len(b), nil
	}

	complete, rest := text, ""
	if last >= 0 {
		complete, rest = text[:last], text[last+1:]
	}

	l.held.Reset()
	l.held.WriteString(rest)
	l.emit(complete)

	return len(b), nil
}

// Flush writes what is left once ffmpeg has ended.
func (l *redactedLog) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.emit(l.held.String())
	l.held.Reset()
}

func (l *redactedLog) emit(text string) {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			fmt.Fprintf(l.out, "%s: %s%s\n", name, l.prefix, redact(line, l.secrets))
		}
	}
}
