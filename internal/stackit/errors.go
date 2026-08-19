package stackit

import (
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
