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
)

type Link struct {
	Path     string
	Fragment string
	Lifetime time.Duration
}

type Options struct {
	Instance    string
	Listen      string
	Build       webclient.Build
	OpenBrowser bool
	Link        *Link
}

type session struct {
	environment *command.Environment
	server      *localclient.Server
	options     Options
	cleanup     func()
	taken       bool
}

func Serve(ctx context.Context, environment *command.Environment, options Options) int {
	listener, err := localclient.Listen(ctx, options.Listen)
	if err != nil {
		return command.Failf(environment, "the verified client could not listen on %s: %s", options.Listen, err)
	}
	notices := make(chan localclient.Notice, noticeBuffer)
	information := environment.BuildInformation()
	server, err := localclient.New(listener.Addr(), localclient.Config{
		Instance:  options.Instance,
		Build:     options.Build,
		Version:   information.Version,
		BuildInfo: information,
		Notify: func(notice localclient.Notice) {
			select {
			case notices <- notice:
			default:
			}
		},
	})
	if err != nil {
		listener.Close()
		return command.Failf(environment, "%s", err)
	}
	httpServer := server.HTTPServer()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	defer shutdown(httpServer)
	current := &session{environment: environment, server: server, options: options, cleanup: func() {}}
	defer func() { current.cleanup() }()
	var expired <-chan time.Time
	if options.Link == nil {
		current.compose()
	} else {
		if err = current.handOver(); err != nil {
			return command.Failf(environment, "%s", err)
		}
		timer := time.NewTimer(options.Link.Lifetime)
		defer timer.Stop()
		expired = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return command.ExitOK
		case err = <-served:
			return command.Failf(environment, "the verified client stopped: %s", err)
		case notice := <-notices:
			current.report(notice)
			if current.taken {
				expired = nil
			}
		case <-expired:
			server.Withdraw()
			return command.Failf(environment, "no browser took the link within %s; run %s open again",
				options.Link.Lifetime, command.Program)
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

func (s *session) handOver() error {
	token, err := s.server.Offer(s.options.Link.Path, s.options.Link.Fragment, s.options.Link.Lifetime)
	if err != nil {
		return err
	}
	address := s.server.Origin() + localclient.HandoffPath + "#" + token
	if s.options.OpenBrowser {
		page, remove, writeErr := writeHandoffPage(address)
		if writeErr == nil {
			s.cleanup = sync.OnceFunc(remove)
			if openErr := browser(s.environment)(page); openErr == nil {
				s.note("opening the message in your browser through the verified client at %s", s.server.Origin())
				return nil
			}
			s.cleanup()
		}
	}
	fmt.Fprintln(s.environment.Stdout, address)
	s.note("open this address in your browser within %s; it works once and carries no part of the link",
		s.options.Link.Lifetime)
	return nil
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
