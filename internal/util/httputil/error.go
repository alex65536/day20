package httputil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Error struct {
	code    int
	message string
	headers map[string][]string
}

func (e *Error) Error() string {
	return fmt.Sprintf("http error %v: %v", e.code, e.message)
}

func (e *Error) Code() int       { return e.code }
func (e *Error) Message() string { return e.message }

func (e *Error) ApplyHeaders(w http.ResponseWriter) {
	if e.headers == nil {
		return
	}
	for k, vs := range e.headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
}

func MakeError(code int, message string) error {
	return &Error{code: code, message: message}
}

func MakeRedirectError(code int, message string, location string) error {
	if !(300 <= code && code <= 399) {
		return MakeError(code, message)
	}
	return &Error{
		code:    code,
		message: message,
		headers: map[string][]string{
			"Location": {location},
		},
	}
}

func MakeAuthError(message string, scheme string) error {
	return &Error{
		code:    http.StatusUnauthorized,
		message: message,
		headers: map[string][]string{"WWW-Authenticate": {scheme}},
	}
}

func ErrorFromResponse(rsp *http.Response) error {
	if 200 <= rsp.StatusCode && rsp.StatusCode <= 299 {
		return nil
	}
	var b strings.Builder
	_, err := io.Copy(&b, rsp.Body)
	return errors.Join(MakeError(rsp.StatusCode, b.String()), err)
}

func WriteErrorResponse(err error, w http.ResponseWriter) error {
	var (
		httpErr *Error
		code    int
		message string
	)
	if errors.As(err, &httpErr) {
		code = httpErr.code
		message = httpErr.message
	} else {
		code = http.StatusInternalServerError
		message = fmt.Sprintf("internal server error: %v", err)
	}
	w.Header().Set("Content-Type", "text/plain")
	if httpErr != nil {
		httpErr.ApplyHeaders(w)
	}
	w.WriteHeader(code)
	if _, err := io.WriteString(w, message); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
