package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/huh"
)

var timedConfirmations atomic.Bool

// TimedConfirmationDelay is fixed so the opt-in mode has a predictable deadline.
const TimedConfirmationDelay = 5 * time.Second

// SetTimedConfirmations scopes the mode to one CLI invocation.
func SetTimedConfirmations(enabled bool) func() {
	previous := timedConfirmations.Swap(enabled)
	return func() { timedConfirmations.Store(previous) }
}

func TimedConfirmationsEnabled() bool { return timedConfirmations.Load() }

// Continue is for routine deployment approvals only. Confirm remains manual.
func Continue(title string) error {
	if !TimedConfirmationsEnabled() {
		return Confirm(title)
	}
	yes := false
	err := runContinuation(continuationForm(title, &yes), &yes, TimedConfirmationDelay)
	if err == nil && !yes {
		fmt.Println("No response after 5 seconds; continuing.")
	}
	return err
}

// Huh also reports terminal read failures as ErrTimeout. Own the deadline so
// only an actual deadline expiration can grant an unattended approval.
func runContinuation(form *huh.Form, yes *bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := form.WithTimeout(0).RunWithContext(ctx)
	expired := errors.Is(ctx.Err(), context.DeadlineExceeded)
	return confirmationResult(*yes, err, expired)
}

func continuationForm(title string, yes *bool) *huh.Form {
	return huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).
		Description("Continuing automatically in 5 seconds. Choose Cancel or press Ctrl+C to stop.").
		Affirmative("Continue").Negative("Cancel").Value(yes))).WithAccessible(false).WithTimeout(TimedConfirmationDelay)
}

func confirmationResult(yes bool, err error, timed bool) error {
	if timed && errors.Is(err, huh.ErrTimeout) {
		return nil
	}
	if err != nil {
		return err
	}
	if !yes {
		return errors.New("cancelled")
	}
	return nil
}

func Required(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("Required")
	}
	return nil
}
func Input(title string, value *string, secret, mandatory bool) *huh.Input {
	field := huh.NewInput().Title(title).Value(value)
	if secret {
		field.EchoMode(huh.EchoModePassword)
	}
	if mandatory {
		field.Validate(Required)
	}
	return field
}
func Confirm(title string) error {
	yes := false
	err := huh.NewConfirm().Title(title).Affirmative("Continue").Negative("Cancel").Value(&yes).Run()
	return confirmationResult(yes, err, false)
}
