package domain

import "time"

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Phone    string `json:"phone,omitempty"`
}

type UserProfile struct {
	ID        int
	UserID    string
	FirstName *string
	LastName  *string
	Phone     *string
	Address   *string
	// CreatedAt/UpdatedAt are read by the Backoffice operator views
	// (RFC-0023); the customer profile path leaves them zero.
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpdateProfileRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}
