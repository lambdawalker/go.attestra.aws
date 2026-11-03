package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/lambdawalker/go.infra.composer/approval"
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

// Composer owns approval policy. The adapter only translates Huh's result.
func runContinuation(form *huh.Form, yes *bool, timeout time.Duration) error {
	return runApproval(approval.Policy{Mode: approval.Timed, Timeout: timeout}, approval.Routine, form, yes)
}

func runApproval(policy approval.Policy, kind approval.Kind, form *huh.Form, yes *bool) error {
	return policy.Confirm(context.Background(), kind, func(ctx context.Context) (bool, error) {
		err := form.WithTimeout(0).RunWithContext(ctx)
		// Huh also uses ErrTimeout for terminal failures. Normalize it only when
		// Composer's context ended, and preserve user aborts and real input errors.
		if err == huh.ErrTimeout && ctx.Err() != nil {
			err = ctx.Err()
		}
		return *yes, err
	})
}

func continuationForm(title string, yes *bool) *huh.Form {
	return huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).
		Description("Continuing automatically in 5 seconds. Choose Cancel or press Ctrl+C to stop.").
		Affirmative("Continue").Negative("Cancel").Value(yes))).WithAccessible(false)
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
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).Affirmative("Continue").Negative("Cancel").Value(&yes))).WithShowHelp(false)
	return runApproval(approval.Policy{}, approval.Sensitive, form, &yes)
}
