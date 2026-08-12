package domain

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
}

type UpdateProfileRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}
