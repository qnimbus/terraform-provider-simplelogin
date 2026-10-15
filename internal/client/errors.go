// SPDX-License-Identifier: MPL-2.0

package client

import (
	"errors"
	"fmt"
	"net/http"
)

// maxErrorMessageLen bounds how much of an API error message ends up in
// diagnostics.
const maxErrorMessageLen = 512

// APIError is returned for any non-2xx response from the SimpleLogin API.
//
// It never contains request headers or bodies, so it is safe to surface in
// diagnostics.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	// Message is the "error" field from the JSON response body, if present.
	Message string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("SimpleLogin API %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if hint := e.hint(); hint != "" {
		msg += " (" + hint + ")"
	}
	return msg
}

func (e *APIError) hint() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		return "the API key is missing, invalid or revoked; check the provider api_key or SIMPLELOGIN_API_KEY"
	case e.StatusCode == http.StatusForbidden:
		return "the API key's account is not allowed to access this object, or the object does not exist"
	case e.StatusCode == http.StatusTooManyRequests:
		return "rate limited by SimpleLogin; wait and retry"
	case e.StatusCode >= 500:
		return "SimpleLogin server error; retry later"
	}
	return ""
}

func statusIs(err error, code int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == code
}

// IsUnauthorized reports whether err is an HTTP 401 from the API.
func IsUnauthorized(err error) bool { return statusIs(err, http.StatusUnauthorized) }

// IsForbidden reports whether err is an HTTP 403 from the API.
func IsForbidden(err error) bool { return statusIs(err, http.StatusForbidden) }

// IsConflict reports whether err is an HTTP 409 from the API.
func IsConflict(err error) bool { return statusIs(err, http.StatusConflict) }

// IsRateLimited reports whether err is an HTTP 429 from the API.
func IsRateLimited(err error) bool { return statusIs(err, http.StatusTooManyRequests) }

// ErrNotFound is returned when an object is confirmed not to exist.
var ErrNotFound = errors.New("not found")
