package payment

import "errors"

type AuthorizationStatus string

const (
	Pending AuthorizationStatus = "pending"
	Success AuthorizationStatus = "success"
	Failure AuthorizationStatus = "failure"
)

type Authorization struct {
	ID     string // UUIDv7
	Status AuthorizationStatus
}

var ErrDeclined = errors.New("payment declined")
