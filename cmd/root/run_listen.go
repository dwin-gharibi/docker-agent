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
func (f *runExecFlags) recallCoordinatorOpt(ctx context.Context, rt runtime.Runtime, sess *session.Session) app.Opt {
	sm := server.NewSessionManager(ctx, nil, rt.SessionStore(), 0, &f.runConfig)
	sm.AttachRuntime(ctx, sess.ID, rt, sess)
	return func(a *app.App) {
		sm.RegisterFollowUpInjector(sess.ID, a.InjectUserMessage)
	}
}

// startSessionCoordinator wires local recall delivery for the in-process
// runtime and, when --listen is set, exposes that runtime over HTTP so
// external processes can drive the running TUI (steer, followup, resume, ...).
// It returns an app.Opt that registers the App as the attached session owner.
func (f *runExecFlags) startSessionCoordinator(ctx context.Context, out *cli.Printer, rt runtime.Runtime, sess *session.Session) (app.Opt, error) {
	if f.listenAddr == "" {
		return f.recallCoordinatorOpt(ctx, rt, sess), nil
	}

	sm := server.NewSessionManager(ctx, nil, rt.SessionStore(), 0, &f.runConfig)
	sm.AttachRuntime(ctx, sess.ID, rt, sess)

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
