package myerrors

import "errors"

func Join(errs ...error) error {
	n := 0
	var err1 error
	for _, err := range errs {
		if err != nil {
			n++
			err1 = err
		}
	}
	switch n {
	case 0:
		return nil
	case 1:
		return err1
	default:
		return errors.Join(errs...)
	}
}
