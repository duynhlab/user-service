package domain

import "errors"

// Sentinel errors for user operations.
var (
	// ErrUserNotFound indicates the requested user does not exist.
	// HTTP Status: 404 Not Found
	ErrUserNotFound = errors.New("user not found")

	// ErrUnauthorized indicates the user is not authorized to perform the operation.
	// HTTP Status: 403 Forbidden
	ErrUnauthorized = errors.New("unauthorized access")
)
