package reason_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/libre/hub/v1"
	"github.com/libre-bots/hub/internal/reason"
)

func TestReasonRoundTrip(t *testing.T) {
	err := reason.Error(codes.NotFound, hubv1.Reason_REASON_GUILD_NOT_REGISTERED, "guild not registered")
	require.Equal(t, codes.NotFound, status.Code(err))
	require.Equal(t, hubv1.Reason_REASON_GUILD_NOT_REGISTERED, reason.Of(err))
	require.Equal(t, hubv1.Reason_REASON_UNSPECIFIED, reason.Of(errors.New("x")))
}

func TestErrorInfoFields(t *testing.T) {
	err := reason.Error(codes.InvalidArgument, hubv1.Reason_REASON_INVALID_ID, "invalid id")
	st := status.Convert(err)
	require.Equal(t, "invalid id", st.Message())
	info, ok := st.Details()[0].(*errdetails.ErrorInfo)
	require.True(t, ok)
	require.Equal(t, "REASON_INVALID_ID", info.Reason)
	require.Equal(t, "hub.libre-bots", info.Domain)
}

func TestOfIgnoresForeignDomain(t *testing.T) {
	st, err := status.New(codes.Internal, "x").WithDetails(&errdetails.ErrorInfo{
		Reason: "REASON_INVALID_ID", Domain: "other",
	})
	require.NoError(t, err)
	require.Equal(t, hubv1.Reason_REASON_UNSPECIFIED, reason.Of(st.Err()))
}

func TestInternalHidesCause(t *testing.T) {
	cause := errors.New("boom")
	err := reason.Internal("register guild", cause)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "register guild", status.Convert(err).Message())
	require.Empty(t, status.Convert(err).Details())
	require.Equal(t, cause, errors.Unwrap(err))
	require.Equal(t, "register guild: boom", err.Error())
	require.Equal(t, hubv1.Reason_REASON_UNSPECIFIED, reason.Of(err))
}

func TestInternalKeepsContextErrors(t *testing.T) {
	require.Equal(t, codes.Canceled, status.Code(reason.Internal("x", context.Canceled)))
	require.Equal(t, codes.DeadlineExceeded, status.Code(reason.Internal("x", context.DeadlineExceeded)))
	wrapped := fmt.Errorf("query: %w", context.Canceled)
	require.Equal(t, codes.Canceled, status.Code(reason.Internal("x", wrapped)))
}

func TestInternalWithCanceledCause(t *testing.T) {
	for _, cause := range []error{context.Canceled, fmt.Errorf("query: %w", context.Canceled)} {
		err := reason.Internal("x", cause)
		require.Equal(t, codes.Canceled, status.Code(err))
		require.Equal(t, "canceled", status.Convert(err).Message())
	}
}

func TestInternalWithDeadlineCause(t *testing.T) {
	cause := fmt.Errorf("query: %w", context.DeadlineExceeded)
	err := reason.Internal("x", cause)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Equal(t, "deadline exceeded", status.Convert(err).Message())
	require.Equal(t, cause, errors.Unwrap(err))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, "x: query: context deadline exceeded", err.Error())
}
