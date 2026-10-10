package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No Collector is needed: the exporter only connects when it has spans to send.
func TestSetupAndShutdown(t *testing.T) {
	shutdown, err := Setup(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NoError(t, shutdown(ctx))
}
