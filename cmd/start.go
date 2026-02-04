package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nuts-foundation/nuts-knooppunt/component"
	libHTTPComponent "github.com/nuts-foundation/nuts-knooppunt/component/http"
	"github.com/nuts-foundation/nuts-knooppunt/component/mcsd"
	"github.com/nuts-foundation/nuts-knooppunt/component/mcsdadmin"
	"github.com/nuts-foundation/nuts-knooppunt/component/status"
	"github.com/nuts-foundation/nuts-knooppunt/component/tracing"
	"github.com/nuts-foundation/nuts-knooppunt/lib/logging"
	"github.com/pkg/errors"
)

func Start(ctx context.Context, config Config) error {
	if !config.StrictMode {
		slog.WarnContext(ctx, "Strict mode is disabled. This is NOT recommended for production environments!")
	}

	publicMux := http.NewServeMux()
	internalMux := http.NewServeMux()

	// Tracing component must be started first to capture logs and spans from other components.
	// We start it immediately (not in the component loop) so that logs from other component
	// constructors (New functions) are also captured via OTLP.
	config.Tracing.ServiceVersion = status.Version()
	tracingComponent := tracing.New(config.Tracing)
	if err := tracingComponent.Start(); err != nil {
		return errors.Wrap(err, "failed to start tracing component")
	}

	mcsdUpdateClient, err := mcsd.New(config.MCSD)
	if err != nil {
		return errors.Wrap(err, "failed to create mCSD Update Client")
	}
	httpComponent := libHTTPComponent.New(config.HTTP, publicMux, internalMux)
	components := []component.Lifecycle{
		mcsdUpdateClient,
		mcsdadmin.New(config.MCSDAdmin),
		status.New(),
		httpComponent,
	}

	// Components: RegisterHandlers()
	for _, cmp := range components {
		cmp.RegisterHttpHandlers(publicMux, internalMux)
	}

	// Components: Start()
	for _, cmp := range components {
		slog.DebugContext(ctx, "Starting component", logging.Component(cmp))
		if err := cmp.Start(); err != nil {
			return errors.Wrapf(err, "failed to start component: %T", cmp)
		}
		slog.DebugContext(ctx, "Component started", logging.Component(cmp))
	}

	slog.DebugContext(ctx, "System started, waiting for shutdown...")
	<-ctx.Done()

	// Components: Stop()
	slog.DebugContext(ctx, "Shutdown signalled, stopping components...")
	for _, cmp := range components {
		slog.DebugContext(ctx, "Stopping component", logging.Component(cmp))
		if err := cmp.Stop(ctx); err != nil {
			slog.ErrorContext(ctx, "Error stopping component", logging.Component(cmp), logging.Error(err))
		}
		slog.DebugContext(ctx, "Component stopped", logging.Component(cmp))
	}
	slog.InfoContext(ctx, "Goodbye!")

	// Stop tracing last to ensure all shutdown logs are captured
	if err := tracingComponent.Stop(ctx); err != nil {
		// Can't use slog here as the handler may already be shut down
		fmt.Printf("Error stopping tracing component: %v\n", err)
	}
	return nil
}