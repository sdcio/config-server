package schema

import (
	"errors"
	"testing"

	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestFailureResult(t *testing.T) {
	boom := errors.New("cannot load schema")

	res, err := failureResult(boom, nil)
	assert.ErrorIs(t, err, boom, "recoverable failures are returned for backoff")
	assert.Zero(t, res)

	res, err = failureResult(nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, schemaPollInterval, res.RequeueAfter, "a schema still loading is polled")

	res, err = failureResult(&sdcerrors.UnrecoverableError{}, nil)
	assert.NoError(t, err, "unrecoverable failures are not retried")
	assert.Zero(t, res)
}
