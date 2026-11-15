package delivery

import (
	"context"
	"errors"

	"github.com/KevinCFechtel/MailNotifyer/internal/notification"
)

// Target receives target-independent notifications.
type Target interface {
	Name() string
	Deliver(context.Context, notification.Notification) error
}

// Error marks whether retrying a delivery can succeed without configuration changes.
type Error struct {
	Err       error
	Permanent bool
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

func Permanent(err error) error {
	return &Error{Err: err, Permanent: true}
}

func Temporary(err error) error {
	return &Error{Err: err}
}

func IsPermanent(err error) bool {
	var deliveryErr *Error
	return err != nil && errors.As(err, &deliveryErr) && deliveryErr.Permanent
}
