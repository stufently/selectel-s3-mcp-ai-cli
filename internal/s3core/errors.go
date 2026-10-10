package s3core

import (
	"errors"
	"fmt"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stufently/selectel-s3-mcp-ai-cli/internal/safety"
)

type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func NotFound(e error) bool {
	var response *smithyhttp.ResponseError
	if errors.As(e, &response) && response.HTTPStatusCode() == 404 {
		return true
	}
	var api smithy.APIError
	return errors.As(e, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey")
}
func MapError(e error, secrets ...string) *Error {
	var own *Error
	if errors.As(e, &own) {
		return &Error{Code: own.Code, Message: safety.Redact(own.Message, secrets...), Retryable: own.Retryable}
	}
	code := "invalid_request"
	var api smithy.APIError
	if errors.As(e, &api) {
		code = api.ErrorCode()
	}
	retryable := false
	var response *smithyhttp.ResponseError
	if errors.As(e, &response) {
		status := response.HTTPStatusCode()
		retryable = status == 429 || status == 503
		if code == "invalid_request" {
			code = fmt.Sprintf("http_%d", status)
		}
	}
	return &Error{Code: code, Message: safety.Redact(e.Error(), secrets...), Retryable: retryable}
}
