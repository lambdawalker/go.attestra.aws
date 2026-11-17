package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

var ErrCancelled = errors.New("cancelled; no default accepted")

type keyEvent struct {
	key byte
	err error
}

// defaultAnswer has no reader goroutines and accepts injected clock channels.
// Its caller must stop/join the input pump before using a timeout result.
func defaultAnswer(events <-chan keyEvent, ticks <-chan time.Time, timeout <-chan time.Time, fallback string, render func(string, int), immediate ...bool) (string, bool, error) {
	text := ""
	remaining := 5
	render(text, remaining)
	for {
		select {
		case e := <-events:
			if len(immediate) > 0 && immediate[0] && e.err == nil {
				switch e.key {
				case 'y', 'Y':
					return "y", false, nil
				case 'n', 'N':
					return "n", false, nil
				}
			}
			if e.err != nil {
				return "", false, fmt.Errorf("%w: %v", ErrCancelled, e.err)
			}
			switch e.key {
			case 3, 4, 27:
				return "", false, ErrCancelled
			case '\r', '\n':
				if text == "" {
					text = fallback
				}
				return text, false, nil
			case 8, 127:
				_, n := utf8.DecodeLastRuneInString(text)
				if n > 0 {
					text = text[:len(text)-n]
				}
			default:
				if e.key >= 32 {
					text += string([]byte{e.key})
				}
			}
			render(text, remaining)
		case <-ticks:
			remaining--
			if remaining > 0 {
				render(text, remaining)
			}
		case <-timeout:
			// A ready error/EOF/answer wins over simultaneous expiry.
			select {
			case e := <-events:
				if e.err != nil || e.key == 3 || e.key == 4 || e.key == 27 {
					return "", false, ErrCancelled
				}
				return "", false, errors.New("input arrived at deadline; answer again")
			default:
			}
			// A partially entered override must not become silent approval.
			if text != "" {
				return "", false, errors.New("input incomplete at deadline; answer again")
			}
			return fallback, true, nil
		}
	}
}

func terminalDefault(title, fallback string) (string, error) {
	return terminalQuestion(title, fallback, false)
}
func terminalQuestion(title, fallback string, immediate bool) (answer string, err error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return "", errors.New("interactive default requires a terminal; EOF never approves")
	}
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return "", err
	}
	defer term.Restore(os.Stdin.Fd(), state)
	reader, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	events := make(chan keyEvent, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var b [1]byte
		for {
			n, e := reader.Read(b[:])
			event := keyEvent{err: e}
			if n > 0 {
				event.key = b[0]
			}
			select {
			case events <- event:
			case <-stop:
				return
			}
			if e != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(TimedConfirmationDelay)
	defer timer.Stop()
	ticks := time.NewTicker(time.Second)
	defer ticks.Stop()
	rich := term.IsTerminal(os.Stdout.Fd()) && os.Getenv("TERM") != "dumb"
	if !rich {
		fmt.Printf("%s [default: %s] Continuing in 5 seconds; Enter accepts, n (for approval), Escape or Ctrl+C cancels.\n", title, fallback)
	}
	render := func(text string, seconds int) {
		if rich {
			fmt.Printf("\r\x1b[2K%s [default: %s] Continuing in %d seconds; Enter accepts, Escape/Ctrl+C cancels: %s", title, fallback, seconds, text)
		}
	}
	answer, timed, result := defaultAnswer(events, ticks.C, timer.C, fallback, render, immediate)
	close(stop)
	if !reader.Cancel() {
		return "", errors.New("terminal cannot cancel pending input safely")
	}
	<-done
	if e := flushInput(os.Stdin.Fd()); e != nil {
		result = e
	}
	// Do not treat terminal closure racing the deadline as silence.
	if timed {
		select {
		case e := <-events:
			if e.err != nil && !errors.Is(e.err, cancelreader.ErrCanceled) {
				result = ErrCancelled
			}
		default:
		}
	}
	if rich {
		fmt.Print("\r\x1b[2K")
	}
	newline := "\n"
	if rich {
		newline = "\r\n"
	}
	if result != nil {
		fmt.Print("Cancelled." + newline)
		return "", result
	}
	fmt.Printf("%s: %s%s", title, answer, newline)
	return answer, nil
}

type TextInput struct {
	*huh.Input
	title             string
	value             *string
	secret, mandatory bool
	validate          func(string) error
}

func (f *TextInput) Validate(check func(string) error) *TextInput {
	f.validate = check
	f.Input.Validate(check)
	return f
}
func (f *TextInput) Run() error {
	if f.secret || !TimedConfirmationsEnabled() {
		return f.Input.Run()
	}
	valid := func(s string) error {
		if f.mandatory && strings.TrimSpace(s) == "" {
			return Required(s)
		}
		if f.validate != nil {
			return f.validate(s)
		}
		return nil
	}
	if valid(*f.value) != nil {
		return f.Input.Run()
	}
	answer, e := terminalDefault(f.title, *f.value)
	if e != nil {
		return e
	}
	if e = valid(answer); e != nil {
		return e
	}
	*f.value = answer
	return nil
}

// Choice is deliberately small: call sites provide validated enumerations.
func Choice(title string, value *string, choices ...string) error {
	answer, e := terminalDefault(title+" ("+strings.Join(choices, "/")+")", *value)
	if e != nil {
		return e
	}
	for _, v := range choices {
		if strings.EqualFold(answer, v) {
			*value = v
			return nil
		}
	}
	return errors.New("invalid choice")
}
