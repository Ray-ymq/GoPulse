package bus

import "errors"

// ErrDuplicate 表示事件投影已经应用，消费者可以确认消息。
var ErrDuplicate = errors.New("projection already applied")

type PermanentError struct {
	Reason string
}

func (err *PermanentError) Error() string { return err.Reason }

func NewPermanentError(reason string) error { return &PermanentError{Reason: reason} }

func IsPermanent(err error) bool {
	var target *PermanentError
	return errors.As(err, &target)
}
