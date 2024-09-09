package http

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type HTTPError struct {
	code    int
	message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http error %v: %v", e.code, e.message)
}

func (e *HTTPError) Code() int { return e.code }

func MakeHTTPError(code int, message string) error {
	return &HTTPError{code: code, message: message}
}

func HTTPErrorFromResponse(rsp *http.Response) error {
	if 200 <= rsp.StatusCode && rsp.StatusCode <= 299 {
		return nil
	}
	var b strings.Builder
	_, err := io.Copy(&b, rsp.Body)
	return errors.Join(MakeHTTPError(rsp.StatusCode, b.String()), err)
}

func WriteErrorResponse(err error, w http.ResponseWriter) error {
	var (
		httpErr *HTTPError
		code    int
		message string
	)
	if errors.As(err, &httpErr) {
		code = httpErr.code
		message = httpErr.message
	} else {
		code = http.StatusInternalServerError
		message = err.Error()
	}
	w.Header().Set("Content-Type", "application/text")
	w.WriteHeader(code)
	if _, err := io.WriteString(w, message); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
