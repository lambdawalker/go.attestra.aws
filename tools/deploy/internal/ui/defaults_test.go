package ui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestDefaultAnswerWithFakeClock(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		timeout, cancel   bool
	}{
		{name: "enter", input: "\r", want: "dev"}, {name: "override", input: "qa\r", want: "qa"}, {name: "timeout", want: "dev", timeout: true}, {name: "escape", input: "\x1b", cancel: true}, {name: "interrupt", input: "\x03", cancel: true}, {name: "closed", input: "\x04", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan keyEvent, len(tc.input))
			for i := range tc.input {
				events <- keyEvent{key: tc.input[i]}
			}
			timeout := make(chan time.Time, 1)
			if tc.timeout {
				timeout <- time.Time{}
			}
			got, _, e := defaultAnswer(events, make(chan time.Time), timeout, "dev", func(string, int) {})
			if (e != nil) != tc.cancel || got != tc.want {
				t.Fatalf("%q %v", got, e)
			}
		})
	}
	for _, e := range []error{io.EOF, io.ErrUnexpectedEOF, errors.New("terminal disconnected")} {
		events := make(chan keyEvent, 1)
		events <- keyEvent{err: e}
		timeout := make(chan time.Time, 1)
		timeout <- time.Time{}
		if _, _, e := defaultAnswer(events, nil, timeout, "yes", func(string, int) {}); e == nil {
			t.Fatal("input failure approved")
		}
	}
}
func TestPartialOverrideCannotAutoApprove(t *testing.T) {
	events := make(chan keyEvent, 1)
	events <- keyEvent{key: 'n'}
	timeout := make(chan time.Time, 1)
	_, _, e := defaultAnswer(events, nil, timeout, "yes", func(text string, _ int) {
		if strings.Contains(text, "n") {
			timeout <- time.Time{}
		}
	})
	if e == nil {
		t.Fatal("incomplete input became approval")
	}
}
