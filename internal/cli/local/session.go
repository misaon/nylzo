package local

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/misaon/nylzo/internal/cli/command"
	"github.com/misaon/nylzo/internal/localclient"
	"github.com/misaon/nylzo/internal/server/webclient"
)

const (
	HandoffLifetime = 2 * time.Minute
	shutdownTimeout = 5 * time.Second
	noticeBuffer    = 8
	reminderShare   = 12
)

type Link struct {
	Path     string
	Fragment string
	Lifetime time.Duration
}

type Options struct {
	Instance          string
	Build             webclient.Build
	OpenBrowser       bool
	Link              *Link
	ResolvesLocalhost func() bool
}

type session struct {
	environment *command.Environment
	server      *localclient.Server
	options     Options
	notices     chan localclient.Notice
	cleanup     func()
	address     string
	taken       bool
}

type deadlines struct {
	expired <-chan time.Time
	remind  <-chan time.Time
	stop    func()
}

func Serve(ctx context.Context, environment *command.Environment, options Options) int {
	listeners, err := localclient.Listen(ctx)
	if err != nil {
		return command.Failf(environment, "the verified client could not listen on a loopback port: %s", err)
	}
	current := &session{
		environment: environment,
		options:     options,
		notices:     make(chan localclient.Notice, noticeBuffer),
		cleanup:     func() {},
	}
	if current.server, err = localclient.New(listeners, current.config()); err != nil {
		listeners.Close()
		return command.Failf(environment, "%s", err)
	}
	httpServer := current.server.HTTPServer()
	served := make(chan error, 1)
	go func() { served <- listeners.Serve(httpServer) }()
	defer shutdown(httpServer)
	defer func() { current.cleanup() }()
	current.keepNameLocal()
	timers, err := current.announce()
	if err != nil {
		return command.Failf(environment, "%s", err)
	}
	defer timers.stop()
	return current.run(ctx, served, timers)
}

func (s *session) config() *localclient.Config {
	information := s.environment.BuildInformation()
	return &localclient.Config{
		Instance:  s.options.Instance,
		Build:     s.options.Build,
		Version:   information.Version,
		BuildInfo: information,
		Notify: func(notice localclient.Notice) {
			select {
			case s.notices <- notice:
			default:
			}
		},
	}
}

func (s *session) keepNameLocal() {
	resolves := s.options.ResolvesLocalhost
	if resolves == nil {
		resolves = systemResolvesLocalhost
	}
	if s.options.OpenBrowser && !resolves() {
		s.options.OpenBrowser = false
		s.note("Safari on macOS before 26 asks the network for names under localhost, so open the address below " +
			"in Chrome, Edge or Firefox, which keep them on this computer")
	}
}

func (s *session) announce() (deadlines, error) {
	timers := deadlines{stop: func() {}}
	if s.options.Link == nil {
		s.compose()
		return timers, nil
	}
	opened, err := s.handOver()
	if err != nil {
		return timers, err
	}
	expiry := time.NewTimer(s.options.Link.Lifetime)
	reminder := time.NewTimer(s.options.Link.Lifetime / reminderShare)
	timers.expired, timers.stop = expiry.C, func() {
		expiry.Stop()
		reminder.Stop()
	}
	if opened {
		timers.remind = reminder.C
	}
	return timers, nil
}

func (s *session) run(ctx context.Context, served <-chan error, timers deadlines) int {
	for {
		select {
		case <-ctx.Done():
			return command.ExitOK
		case err := <-served:
			return command.Failf(s.environment, "the verified client stopped: %s", err)
		case notice := <-s.notices:
			s.report(notice)
			if s.taken {
				timers.expired, timers.remind = nil, nil
			}
		case <-timers.remind:
			s.remind()
		case <-timers.expired:
			s.server.Withdraw()
			return command.Failf(s.environment, "no browser took the link within %s; run %s open again",
				s.options.Link.Lifetime, command.Program)
		}
	}
}

func (s *session) compose() {
	address := s.server.Origin() + "/"
	fmt.Fprintln(s.environment.Stdout, address)
	s.note("the verified client runs at %s and sends only API calls to %s; links it creates point to %s. "+
		"Press Ctrl-C to stop it", address, s.server.Instance(), s.server.Instance())
	if s.options.OpenBrowser {
		if err := browser(s.environment)(address); err != nil {
			s.note("no browser opened; open %s yourself", address)
		}
	}
}

func (s *session) handOver() (bool, error) {
	token, err := s.server.Offer(s.options.Link.Path, s.options.Link.Fragment, s.options.Link.Lifetime)
	if err != nil {
		return false, err
	}
	s.address = s.server.Origin() + localclient.HandoffPath + "#" + token
	if s.options.OpenBrowser {
		page, writeErr := writeHandoffFile(s.address)
		if writeErr == nil {
			s.cleanup = sync.OnceFunc(page.remove)
			if openErr := browser(s.environment)(page.path); openErr == nil {
				s.note("opening the message in your browser through the verified client at %s", s.server.Origin())
				return true, nil
			}
			s.cleanup()
		}
	}
	fmt.Fprintln(s.environment.Stdout, s.address)
	s.note("open this address in your browser within %s; it works once and carries no part of the link",
		s.options.Link.Lifetime)
	return false, nil
}

// remind gives the address to a user whose browser could not read the handoff file, such as a confined one.
func (s *session) remind() {
	fmt.Fprintln(s.environment.Stdout, s.address)
	s.note("no browser has taken the link yet. If none opened the message, open this address in your browser; " +
		"it works once and carries no part of the link")
}

func (s *session) report(notice localclient.Notice) {
	switch notice {
	case localclient.HandoffTaken:
		s.taken = true
		s.cleanup()
		s.note("the browser took the link and the handoff address no longer works. " +
			"Press Ctrl-C to stop the verified client when you are done reading")
	case localclient.HandoffRefused:
		if s.taken {
			s.note("warning: something asked for the link again after a browser took it. If your browser " +
				"did not show the message, another program has the link: delete the message with nylzo burn " +
				"and ask the sender for a new one")
		}
	default:
	}
}

func (s *session) note(format string, arguments ...any) {
	fmt.Fprintf(s.environment.Stderr, command.Program+": "+format+"\n", arguments...)
}

func shutdown(httpServer *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpServer.Shutdown(ctx); errors.Is(err, context.DeadlineExceeded) {
		httpServer.Close()
	}
}
