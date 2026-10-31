package domain

import (
	"errors"
	"fmt"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
)

// cakeMessageFor checks a line's message on the cake against its product's
// settings (F2) and returns it trimmed, or nil for none. requireFilled is
// false for staff-entered carts, which may leave a required message to be
// confirmed with the customer.
func cakeMessageFor(opts catalogapi.CakeOptions, raw *string, requireFilled bool) (*string, error) {
	msg := ""
	if raw != nil {
		msg = strings.TrimSpace(*raw)
	}
	if err := opts.CheckCakeMessage(msg, requireFilled); err != nil {
		return nil, cakeMessageError(opts, err)
	}
	if msg == "" {
		return nil, nil
	}
	return &msg, nil
}

// cakeMessageError is the customer-facing form of a message rule failure.
func cakeMessageError(opts catalogapi.CakeOptions, err error) error {
	switch {
	case errors.Is(err, catalogapi.ErrCakeMessageRequired):
		return apperr.New("CAKE_MESSAGE_REQUIRED", apperr.CategoryValidation, "Please add the message for the cake.").
			WithFields(apperr.FieldError{Field: "cakeMessage", Code: "REQUIRED", Message: "Required."})
	case errors.Is(err, catalogapi.ErrCakeMessageTooLong):
		msg := fmt.Sprintf("The message on the cake can be at most %d characters.", opts.EffectiveMaxLength())
		return apperr.New("CAKE_MESSAGE_TOO_LONG", apperr.CategoryValidation, msg).
			WithFields(apperr.FieldError{Field: "cakeMessage", Code: "TOO_LONG", Message: msg})
	case errors.Is(err, catalogapi.ErrCakeMessageNotAllowed):
		return apperr.New("CAKE_MESSAGE_NOT_ALLOWED", apperr.CategoryValidation, "This item doesn't take a message on the cake.").
			WithFields(apperr.FieldError{Field: "cakeMessage", Code: "NOT_ALLOWED", Message: "Not offered for this item."})
	default:
		return apperr.Internal(err)
	}
}

// apperrMessage is an error's customer-facing sentence.
func apperrMessage(err error) string {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}
