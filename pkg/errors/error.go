/*
Copyright 2024 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package errors

import "fmt"

type RecoverableError struct {
	Message      string
	WrappedError error
}

func (e *RecoverableError) Error() string {
	if e.WrappedError != nil {
		return fmt.Sprintf("%s, err: %s", e.Message, e.WrappedError.Error())
	}
	return e.Message
}

func (e *RecoverableError) Unwrap() error {
	return e.WrappedError
}

type UnrecoverableError struct {
	Message      string
	WrappedError error
}

func (e *UnrecoverableError) Error() string {
	if e.WrappedError != nil {
		return fmt.Sprintf("%s, err: %s", e.Message, e.WrappedError.Error())
	}
	return e.Message
}

func (e *UnrecoverableError) Unwrap() error {
	return e.WrappedError
}
