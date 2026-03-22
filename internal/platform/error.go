package platform

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalidArgument Code = "invalid_argument"
	CodeParse           Code = "parse_error"
	CodeValidation      Code = "validation_error"
	CodeIO              Code = "io_error"
	CodeNotFound        Code = "not_found"
	CodeInternal        Code = "internal_error"
)

type Error struct {
	Code    Code
	Message string
	Err     error
}

func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	var coded *Error
	if !errors.As(target, &coded) {
		return false
	}
	return e.Code == coded.Code
}

func (e *Error) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Err == nil:
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	case e.Message == "":
		return fmt.Sprintf("%s: %v", e.Code, e.Err)
	default:
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

func Wrap(code Code, err error, message string) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Message: message, Err: err}
}

func CodeOf(err error) Code {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return CodeInternal
}

func As(err error) *Error {
	var coded *Error
	if errors.As(err, &coded) {
		return coded
	}
	return nil
}

func IsCode(err error, code Code) bool {
	return errors.Is(err, &Error{Code: code})
}
