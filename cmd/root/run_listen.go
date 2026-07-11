package root

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/docker/docker-agent/pkg/app"
	"github.com/docker/docker-agent/pkg/cli"
	"github.com/docker/docker-agent/pkg/runregistry"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/server"
	"github.com/docker/docker-agent/pkg/session"
)

// recallCoordinatorOpt wires the session manager's recall handler to this
// in-process runtime even when the HTTP control plane is disabled. That lets a
// background tool wake an idle local TUI by routing through the same injector
// used by control-plane follow-ups.
//
// It also wires app.WithStreamGuard so the App's own direct RunStream calls
// (Run/Retry/RunWithMessage) hold the same lock RunSession/AddMessage/
// UpdateMessage use to detect an active stream, closing the gap where a
// concurrent REST mutation could slip in during an attached/TUI stream (#3590).
func (f *runExecFlags) recallCoordinatorOpt(ctx context.Context, rt runtime.Runtime, sess *session.Session) app.Opt {
	sm := server.NewSessionManager(ctx, nil, rt.SessionStore(), 0, &f.runConfig)
	guard := sm.AttachRuntime(ctx, sess.ID, rt, sess)
	return func(a *app.App) {
		app.WithStreamGuard(guard)(a)
		sm.RegisterFollowUpInjector(sess.ID, a.InjectUserMessage)
	}
}

// startSessionCoordinator wires local recall delivery for the in-process
// runtime and, when --listen is set, exposes that runtime over HTTP so
// external processes can drive the running TUI (steer, followup, resume, ...).
// It returns an app.Opt that registers the App as the attached session owner
// (see recallCoordinatorOpt for the stream-guard wiring shared by both paths).
func (f *runExecFlags) startSessionCoordinator(ctx context.Context, out *cli.Printer, rt runtime.Runtime, sess *session.Session) (app.Opt, error) {
	if f.listenAddr == "" {
		return f.recallCoordinatorOpt(ctx, rt, sess), nil
	}

	sm := server.NewSessionManager(ctx, nil, rt.SessionStore(), 0, &f.runConfig)
	guard := sm.AttachRuntime(ctx, sess.ID, rt, sess)

	ln, err := server.Listen(ctx, f.listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", f.listenAddr, err)
	}
	context.AfterFunc(ctx, func() { _ = ln.Close() })

	cleanup, err := runregistry.Default().Write(runregistry.Record{
		PID:       os.Getpid(),
		Addr:      "http://" + ln.Addr().String(),
		SessionID: sess.ID,
		Agent:     f.agentName,
		StartedAt: time.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "Could not write run registry record", "error", err)
	} else {
		context.AfterFunc(ctx, cleanup)
	}

	out.Println("Control plane listening on", ln.Addr().String())
	warnIfNotLoopback(out, ln.Addr())

	srv := server.NewWithManager(sm, "")
	go func() {
		if err := srv.Serve(ctx, ln); err != nil {
			slog.ErrorContext(ctx, "Control plane server stopped", "error", err)
		}
	}()

	return func(a *app.App) {
		app.WithStreamGuard(guard)(a)
		sm.RegisterEventSource(sess.ID, func(ctx context.Context, send func(any)) {
			a.SubscribeWith(ctx, func(msg tea.Msg) {
				if ev, ok := msg.(runtime.Event); ok {
					send(ev)
				}
			})
		})
		// Route control-plane follow-ups and idle recalls into the TUI App so
		// each starts a real turn and streams events to every subscriber.
		sm.RegisterFollowUpInjector(sess.ID, a.InjectUserMessage)
	}, nil
}
