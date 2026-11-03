package ui

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func TestContinuationFormTimeoutAndAnswers(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		cancel      bool
	}{
		{"timeout", "", false}, {"continue", "y", false}, {"cancel", "n", true}, {"interrupt", "\x03", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yes := false
			form := continuationForm("Deploy?", &yes).WithOutput(io.Discard).WithProgramOptions(tea.WithoutRenderer())

			delay := time.Second
			if tc.input == "" {
				form.WithInput(nil)
				delay = 20 * time.Millisecond
			} else {
				form.WithInput(strings.NewReader(tc.input))
			}
			err := runContinuation(form, &yes, delay)
			if tc.input == "y" && !yes {
				t.Fatal("answer lost")
			}
			if (err != nil) != tc.cancel {
				t.Fatalf("cancel=%v, got %v", tc.cancel, err)
			}

		})
	}
}

func TestConfirmationOnlyTimeoutCanAutoContinue(t *testing.T) {
	for _, err := range []error{huh.ErrUserAborted, huh.ErrTimeoutUnsupported, errors.New("terminal unavailable")} {
		if got := confirmationResult(false, err, true); !errors.Is(got, err) {
			t.Fatal("ignored error", err, got)
		}
	}
	if confirmationResult(false, nil, true) == nil {
		t.Fatal("ignored Cancel")
	}
	if confirmationResult(false, huh.ErrTimeout, false) == nil {
		t.Fatal("manual prompt auto-approved")
	}
	if TimedConfirmationDelay != 5*time.Second {
		t.Fatal(TimedConfirmationDelay)
	}
	restore := SetTimedConfirmations(true)
	if !TimedConfirmationsEnabled() {
		t.Fatal("mode not enabled")
	}
	restore()
	if TimedConfirmationsEnabled() {
		t.Fatal("mode leaked")
	}
}

type failedTerminalReader struct{}

func (failedTerminalReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestTerminalFailureNeverAutoApproves(t *testing.T) {
	yes := false
	form := continuationForm("Deploy?", &yes).WithProgramOptions(tea.WithoutRenderer()).WithInput(failedTerminalReader{}).WithOutput(io.Discard)
	err := runContinuation(form, &yes, time.Second)
	if err == nil {
		t.Fatal("terminal failure became deployment approval")
	}
}
