package util

import (
	"sync"
)

const MaxParallelValidations = 5

type ValidatorFunc func(string) error

// Validate will invoke the validator block once for each of the provided codes
// The validator argument will take one code at a time and should return an error if the validation failed
// The validator block will be invoked asynchronously in chunks of the size of parallel
// In case there are multiple errors in a chunk, then only the last error will be returned
func Validate(codes []string, parallel int, validator ValidatorFunc) error {
	var err error
	for _, chunk := range Chunk(codes, parallel) {
		var wg sync.WaitGroup
		wg.Add(len(chunk))

		for _, code := range chunk {
			go func(code string) {
				defer wg.Done()
				if validatorErr := validator(code); validatorErr != nil {
					err = validatorErr
				}
			}(code)
		}

		wg.Wait()
		if err != nil {
			return err
		}
	}
	return nil
}
