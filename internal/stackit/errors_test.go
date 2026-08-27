package stackit

import (
	"errors"
	"testing"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain error", errors.New("boom"), false},
		{"404 error", oapierror.NewError(404, "not found"), true},
		{"wrapped 404 error", errWrap{oapierror.NewError(404, "not found")}, true},
		{"500 error", oapierror.NewError(500, "server error"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsTransientAuthError(t *testing.T) {
	invalidGrantBody := []byte(`{"error":"invalid_grant","error_description":"invalid iat"}`)
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"plain error", errors.New("boom"), false},
		{"invalid_grant 400 error", oapierror.NewErrorWithBody(400, "", invalidGrantBody, nil), true},
		{"wrapped invalid_grant 400 error", errWrap{oapierror.NewErrorWithBody(400, "", invalidGrantBody, nil)}, true},
		{"unrelated 400 error", oapierror.NewErrorWithBody(400, "", []byte(`{"error":"invalid_request"}`), nil), false},
		{"404 error", oapierror.NewError(404, "not found"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTransientAuthError(tt.err); got != tt.want {
				t.Errorf("IsTransientAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// errWrap wraps an error to exercise errors.As unwrapping in IsNotFound.
type errWrap struct{ err error }

func (e errWrap) Error() string { return e.err.Error() }
func (e errWrap) Unwrap() error { return e.err }
