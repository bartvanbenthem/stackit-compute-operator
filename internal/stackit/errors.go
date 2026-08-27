package stackit

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/stackitcloud/stackit-sdk-go/core/oapierror"
)

// IsNotFound reports whether err is a STACKIT API "not found" (HTTP 404)
// error, e.g. because a resource has already been deleted out of band.
func IsNotFound(err error) bool {
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) {
		return oapiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// IsConflict reports whether err is a STACKIT API "conflict" (HTTP 409)
// error, e.g. because a resource has a dependent that has not finished
// deleting yet. Callers on the delete path should treat this as an expected,
// transient condition and requeue rather than surface it as a hard error.
func IsConflict(err error) bool {
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) {
		return oapiErr.StatusCode == http.StatusConflict
	}
	return false
}

// IsTransientAuthError reports whether err is STACKIT's token endpoint
// rejecting the SDK's authentication assertion with "invalid_grant"/"invalid
// iat". In practice this happens when the local clock is briefly skewed
// relative to STACKIT's (e.g. a WSL2 guest clock catching up right after the
// host wakes), and it reliably clears on the very next token request once
// the clock has settled. Callers should log this at a lower severity and
// requeue rather than surface it as a hard reconcile error.
func IsTransientAuthError(err error) bool {
	var oapiErr *oapierror.GenericOpenAPIError
	if errors.As(err, &oapiErr) {
		return oapiErr.StatusCode == http.StatusBadRequest && bytes.Contains(oapiErr.Body, []byte("invalid_grant"))
	}
	return false
}
