package reason

import (
	"context"
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
)

const Domain = "hub.libre-bots"

// Error returns a status error that carries r as an ErrorInfo detail. msg is for logs, not for users.
func Error(code codes.Code, r hubv1.Reason, msg string) error {
	st, err := status.New(code, msg).WithDetails(&errdetails.ErrorInfo{Reason: r.String(), Domain: Domain})
	if err != nil {
		return status.Error(code, msg)
	}
	return st.Err()
}

// Of returns the reason of err, or REASON_UNSPECIFIED when err carries none.
func Of(err error) hubv1.Reason {
	st, ok := status.FromError(err)
	if !ok {
		return hubv1.Reason_REASON_UNSPECIFIED
	}
	for _, d := range st.Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok || info.Domain != Domain {
			continue
		}
		return hubv1.Reason(hubv1.Reason_value[info.Reason])
	}
	return hubv1.Reason_REASON_UNSPECIFIED
}

type internalError struct {
	code   codes.Code
	msg    string
	public string
	cause  error
}

func (e *internalError) Error() string { return e.msg + ": " + e.cause.Error() }

func (e *internalError) Unwrap() error { return e.cause }

func (e *internalError) GRPCStatus() *status.Status { return status.New(e.code, e.public) }

// Internal reports an unexpected failure: the caller sees msg, never the cause. Context errors keep their code.
func Internal(msg string, cause error) error {
	switch {
	case errors.Is(cause, context.Canceled):
		return status.Error(codes.Canceled, "canceled")
	case errors.Is(cause, context.DeadlineExceeded):
		return &internalError{code: codes.DeadlineExceeded, msg: msg, public: "deadline exceeded", cause: cause}
	}
	return &internalError{code: codes.Internal, msg: msg, public: msg, cause: cause}
}
